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

type UDPDatagram struct {
	Addr netip.AddrPort
	data []byte
}

func (d UDPDatagram) ContextID() uint64 {
	id, _, _ := quicvarint.Parse(d.data)
	return id
}

func (d UDPDatagram) Payload() []byte {
	_, n, _ := quicvarint.Parse(d.data)
	return d.data[n:]
}

type UDPDatagramReader interface {
	ReadDatagram(ctx context.Context) (UDPDatagram, error)
}
type UDPDatagramWriter interface {
	WriteDatagram(ctx context.Context, datagram UDPDatagram) error
}
type UDPDatagramReaderWriter interface {
	UDPDatagramReader
	UDPDatagramWriter
}

type UDPTunneler interface {
	TunnelUDP(
		ctx context.Context,
		eventSink EventSink,
		local UDPDatagramReaderWriter,
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
	return newUDPSessionManager(conn, func(ctx context.Context, urw UDPDatagramReaderWriter) error {
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

type udpSessionHandler func(context.Context, UDPDatagramReaderWriter) error

type udpSessionManager struct {
	conn    *net.UDPConn
	handler udpSessionHandler
	in      chan UDPDatagram
	out     chan UDPDatagram
}

func newUDPSessionManager(conn *net.UDPConn, handler udpSessionHandler) *udpSessionManager {
	return &udpSessionManager{
		conn:    conn,
		handler: handler,
		in:      make(chan UDPDatagram, 1),
		out:     make(chan UDPDatagram, 1),
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

	buffer := make([]byte, len(contextIDZero)+maxUDPPacketSize)
	for {
		n, addr, err := mgr.conn.ReadFromUDP(buffer[len(contextIDZero):])
		if err != nil {
			// if this error is because the context was cancelled, return that instead
			select {
			case <-ctx.Done():
				return context.Cause(ctx)
			default:
			}
			return fmt.Errorf("udp-session-manager: error reading udp packet: %w", err)
		}
		datagram := UDPDatagram{Addr: addr.AddrPort(), data: make([]byte, len(contextIDZero)+n)}
		copy(datagram.data, buffer)

		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case mgr.in <- datagram:
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
		case datagram := <-mgr.in:
			s, ok := sessions[datagram.Addr]
			if !ok {
				s = newUDPSession(mgr, datagram.Addr)
				go func() {
					_ = s.run(ctx)
					select {
					case <-ctx.Done():
					case stopped <- s:
					}
				}()
				sessions[datagram.Addr] = s
			}
			s.HandleDatagram(ctx, datagram)
		case s := <-stopped:
			delete(sessions, s.addr)
		}
	}
}

func (mgr *udpSessionManager) write(ctx context.Context) error {
	// if the context is cancelled, cancel the write
	context.AfterFunc(ctx, func() { _ = mgr.conn.SetWriteDeadline(time.Now()) })

	for {
		var datagram UDPDatagram
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case datagram = <-mgr.out:
		}

		_, err := mgr.conn.WriteToUDP(datagram.Payload(), net.UDPAddrFromAddrPort(datagram.Addr))
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
	in   chan UDPDatagram

	cancel    context.CancelCauseFunc
	cancelCtx context.Context
}

func newUDPSession(mgr *udpSessionManager, addr netip.AddrPort) *udpSession {
	s := &udpSession{
		mgr:  mgr,
		addr: addr,
		in:   make(chan UDPDatagram, 1),
	}
	s.cancelCtx, s.cancel = context.WithCancelCause(context.Background())
	return s
}

func (s *udpSession) HandleDatagram(ctx context.Context, datagram UDPDatagram) {
	select {
	case <-ctx.Done():
	case <-s.cancelCtx.Done():
	case s.in <- datagram:
	}
}

func (s *udpSession) ReadDatagram(ctx context.Context) (UDPDatagram, error) {
	select {
	case <-ctx.Done():
		return UDPDatagram{}, context.Cause(ctx)
	case datagram := <-s.in:
		return datagram, nil
	}
}

func (s *udpSession) WriteDatagram(ctx context.Context, datagram UDPDatagram) error {
	// rewrite the address
	datagram.Addr = s.addr
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case s.mgr.out <- datagram:
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

	return io.ReadAll(r)
}

func streamFromCapsuleDatagramsToUDPDatagramWriter(
	ctx context.Context,
	dst UDPDatagramWriter,
	src io.Reader,
) error {
	br := bufio.NewReader(src)
	for {
		data, err := readUDPCapsuleDatagram(br)
		if err != nil {
			return err
		}

		err = dst.WriteDatagram(ctx, UDPDatagram{data: data})
		if err != nil {
			return err
		}
	}
}

func streamFromUDPDatagramReaderToCapsuleDatagrams(
	ctx context.Context,
	dst io.Writer,
	src UDPDatagramReader,
) error {
	bw := bufio.NewWriter(dst)
	for {
		datagram, err := src.ReadDatagram(ctx)
		if err != nil {
			return err
		}

		err = http3.WriteCapsule(bw, 0, datagram.data)
		if err != nil {
			return err
		}

		err = bw.Flush()
		if err != nil {
			return err
		}
	}
}
