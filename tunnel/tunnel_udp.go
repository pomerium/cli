package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

const maxUDPPacketSize = (2 << 15) - 1

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
		tunneler := &http1tunneler{cfg: tun.cfg}
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
			if dropped := dropAll(mgr.in); dropped > 0 {
				log.Ctx(ctx).Error().Int("count", dropped).Msg("dropped inbound packets")
			}
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
			if dropped := sendOrDrop(s.in, packet); dropped > 0 {
				log.Ctx(ctx).Error().Int("count", dropped).Msg("dropped session packets")
			}
		case s := <-stopped:
			delete(sessions, s.addr)
			if dropped := dropAll(s.in); dropped > 0 {
				log.Ctx(ctx).Error().Int("count", dropped).Msg("dropped session packets")
			}
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
			if dropped := dropAll(mgr.out); dropped > 0 {
				log.Ctx(ctx).Error().Int("count", dropped).Msg("dropped outbound packets")
			}
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
}

func newUDPSession(mgr *udpSessionManager, addr netip.AddrPort) *udpSession {
	return &udpSession{
		mgr:  mgr,
		addr: addr,
		in:   make(chan UDPPacket, 128),
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
	return err
}

func dropAll[T any](ch chan T) (dropped int) {
	for {
		select {
		case <-ch:
			dropped++
		default:
			return dropped
		}
	}
}

func sendOrDrop[T any](ch chan T, packet T) (dropped int) {
	for {
		select {
		case ch <- packet:
			return dropped
		default:
		}

		if cap(ch) == 0 {
			dropped++
			return dropped
		}

		select {
		case <-ch:
			dropped++
		default:
		}
	}
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
