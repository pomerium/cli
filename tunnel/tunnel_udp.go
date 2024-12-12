package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

const maxUDPPacketSize = (2 << 15) - 1

var contextIDZero = quicvarint.Append(nil, 0)

type UDPPacket struct {
	Addr    netip.AddrPort
	Payload []byte
}

type UDPPacketReader interface {
	ReadPacket(ctx context.Context) (UDPPacket, error)
}
type UDPPacketWriter interface {
	WritePacket(ctx context.Context, packet UDPPacket) error
}
type UDPPacketReaderWriter interface {
	UDPPacketReader
	UDPPacketWriter
}

type UDPTunneler interface {
	TunnelUDP(
		ctx context.Context,
		eventSink EventSink,
		local UDPPacketReaderWriter,
		rawJWT string,
	) error
}

// PickUDPTunneler picks a UDP tunneler for the given proxy.
func (tun *Tunnel) pickUDPTunneler(ctx context.Context) UDPTunneler {
	ctx = log.Ctx(ctx).With().Str("component", "pick-UDP-tunneler").Logger().WithContext(ctx)

	fallback := &http1tunneler{cfg: tun.cfg}

	// if we're not using TLS, only HTTP1 is supported
	if tun.cfg.tlsConfig == nil {
		log.Ctx(ctx).Info().Msg("tls not enabled, using http1")
		return fallback
	}

	client := &http.Client{
		Transport: &http.Transport{
			ForceAttemptHTTP2: true,
			TLSClientConfig:   tun.cfg.tlsConfig,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+tun.cfg.proxyHost, nil)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to create probe request, falling back to http1")
		return fallback
	}

	res, err := client.Do(req)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to make probe request, falling back to http1")
		return fallback
	}
	res.Body.Close()

	if v := res.Header.Get("Alt-Svc"); strings.Contains(v, "h3") {
		log.Ctx(ctx).Info().Msg("using http3")
		return &http3tunneler{cfg: tun.cfg}
	}

	log.Ctx(ctx).Info().Msg("using http1")
	return fallback
}

func (tun *Tunnel) RunUDPListener(ctx context.Context, listenerAddress string) error {
	ctx = log.Ctx(ctx).With().Str("listener-addr", listenerAddress).Logger().WithContext(ctx)

	addr, err := net.ResolveUDPAddr("udp", listenerAddress)
	if err != nil {
		return fmt.Errorf("udp-tunnel: failed to resolve udp address: %w", err)
	}

	log.Ctx(ctx).Info().Msg("starting udp listener")
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("udp-tunnel: failed to listen on udp address: %w", err)
	}
	defer conn.Close()

	err = tun.RunUDPSessionManager(ctx, conn)
	log.Ctx(ctx).Error().Err(err).Msg("stopped udp listener")
	return err
}

func (tun *Tunnel) RunUDPSessionManager(ctx context.Context, conn *net.UDPConn) error {
	return newUDPSessionManager(conn, func(ctx context.Context, urw UDPPacketReaderWriter) error {
		tunneler := tun.pickUDPTunneler(ctx)
		eventSink := LogEvents()
		return tun.runWithJWT(ctx, eventSink, func(ctx context.Context, rawJWT string) error {
			// always disconnect after 10 minutes
			ctx, clearTimeout := context.WithTimeout(ctx, 10*time.Minute)
			defer clearTimeout()

			return tunneler.TunnelUDP(ctx, eventSink, urw, rawJWT)
		})
	}).run(ctx)
}

type udpSessionHandler func(context.Context, UDPPacketReaderWriter) error

type udpSessionManager struct {
	conn    *net.UDPConn
	handler udpSessionHandler
	in      chan UDPPacket
	out     chan UDPPacket
}

func newUDPSessionManager(conn *net.UDPConn, handler udpSessionHandler) *udpSessionManager {
	return &udpSessionManager{
		conn:    conn,
		handler: handler,
		in:      make(chan UDPPacket, 1),
		out:     make(chan UDPPacket, 1),
	}
}

func (mgr *udpSessionManager) run(ctx context.Context) error {
	log.Ctx(ctx).Info().Msg("starting udp session manager")
	eg, ectx := errgroup.WithContext(ctx)
	eg.Go(func() error { return mgr.read(ectx) })
	eg.Go(func() error { return mgr.dispatch(ectx) })
	eg.Go(func() error { return mgr.write(ectx) })
	err := eg.Wait()
	log.Ctx(ctx).Error().Err(err).Msg("stopped udp session manager")
	return err
}

func (mgr *udpSessionManager) read(ctx context.Context) error {
	// if the context is cancelled, cancel the read
	context.AfterFunc(ctx, func() { _ = mgr.conn.SetReadDeadline(time.Now()) })

	var buffer [maxUDPPacketSize]byte
	for {
		n, addr, err := mgr.conn.ReadFromUDP(buffer[:])
		if err != nil {
			// if this error is because the context was cancelled, return that instead
			select {
			case <-ctx.Done():
				return context.Cause(ctx)
			default:
			}
			return fmt.Errorf("udp-session-manager: error reading udp packet: %w", err)
		}
		packet := UDPPacket{Addr: addr.AddrPort(), Payload: make([]byte, n)}
		copy(packet.Payload, buffer[:n])

		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case mgr.in <- packet:
		}
	}
}

func (mgr *udpSessionManager) dispatch(ctx context.Context) error {
	sessions := make(map[netip.AddrPort]*udpSession)
	stopped := make(chan *udpSession)
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case packet := <-mgr.in:
			s, ok := sessions[packet.Addr]
			if !ok {
				s = newUDPSession(mgr, packet.Addr)
				go func() {
					_ = s.run(ctx)
					select {
					case <-ctx.Done():
					case stopped <- s:
					}
				}()
				sessions[packet.Addr] = s
			}
			s.HandlePacket(ctx, packet)
		case s := <-stopped:
			delete(sessions, s.addr)
		}
	}
}

func (mgr *udpSessionManager) write(ctx context.Context) error {
	// if the context is cancelled, cancel the write
	context.AfterFunc(ctx, func() { _ = mgr.conn.SetWriteDeadline(time.Now()) })

	for {
		var packet UDPPacket
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case packet = <-mgr.out:
		}

		_, err := mgr.conn.WriteToUDP(packet.Payload, net.UDPAddrFromAddrPort(packet.Addr))
		if err != nil {
			// if this error is because the context was cancelled, return that instead
			select {
			case <-ctx.Done():
				return context.Cause(ctx)
			default:
			}
			return fmt.Errorf("udp-session-manager: error writing udp packet: %w", err)
		}
	}
}

type udpSession struct {
	mgr  *udpSessionManager
	addr netip.AddrPort
	in   chan UDPPacket

	cancel    context.CancelCauseFunc
	cancelCtx context.Context
}

func newUDPSession(mgr *udpSessionManager, addr netip.AddrPort) *udpSession {
	s := &udpSession{
		mgr:  mgr,
		addr: addr,
		in:   make(chan UDPPacket, 1),
	}
	s.cancelCtx, s.cancel = context.WithCancelCause(context.Background())
	return s
}

func (s *udpSession) HandlePacket(ctx context.Context, packet UDPPacket) {
	select {
	case <-ctx.Done():
	case <-s.cancelCtx.Done():
	case s.in <- packet:
	}
}

func (s *udpSession) ReadPacket(ctx context.Context) (UDPPacket, error) {
	select {
	case <-ctx.Done():
		return UDPPacket{}, context.Cause(ctx)
	case packet := <-s.in:
		return packet, nil
	}
}

func (s *udpSession) WritePacket(ctx context.Context, packet UDPPacket) error {
	// rewrite the address
	packet.Addr = s.addr
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case s.mgr.out <- packet:
	}
	return nil
}

func (s *udpSession) run(ctx context.Context) error {
	ctx = log.Ctx(ctx).With().Str("addr", s.addr.String()).Logger().WithContext(ctx)

	log.Ctx(ctx).Info().Msg("starting udp session")
	err := s.mgr.handler(ctx, s)
	log.Ctx(ctx).Error().Err(err).Msg("stopped udp session")
	s.cancel(err)
	return err
}

func readUDPCapsuleDatagram(
	src quicvarint.Reader,
) ([]byte, error) {
	_, r, err := http3.ParseCapsule(src)
	if err != nil {
		return nil, err
	}

	// ignore the datagram type
	_, err = quicvarint.Read(quicvarint.NewReader(r))
	if err != nil {
		return nil, err
	}

	return io.ReadAll(r)
}

var contextID = quicvarint.Append(nil, 0)

func writeUDPCapsuleDatagram(
	dst quicvarint.Writer,
	packet []byte,
) error {
	payload := make([]byte, 0, len(packet)+len(contextID))
	payload = append(payload, contextID...)
	payload = append(payload, packet...)

	return http3.WriteCapsule(dst, 0, payload)
}

func streamFromCapsuleDatagramsToUDPPacketWriter(
	ctx context.Context,
	dst UDPPacketWriter,
	src io.Reader,
) error {
	br := bufio.NewReader(src)
	for {
		payload, err := readUDPCapsuleDatagram(br)
		if err != nil {
			return err
		}

		err = dst.WritePacket(ctx, UDPPacket{Payload: payload})
		if err != nil {
			return err
		}
	}
}

func streamFromUDPPacketReaderToCapsuleDatagrams(
	ctx context.Context,
	dst io.Writer,
	src UDPPacketReader,
) error {
	bw := bufio.NewWriter(dst)
	for {
		packet, err := src.ReadPacket(ctx)
		if err != nil {
			return err
		}

		err = writeUDPCapsuleDatagram(bw, packet.Payload)
		if err != nil {
			return err
		}

		err = bw.Flush()
		if err != nil {
			return err
		}
	}
}
