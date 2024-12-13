package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"

	"github.com/dunglas/httpsfv"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

type http3tunneler struct {
	cfg *config
}

func (t *http3tunneler) TunnelTCP(
	ctx context.Context,
	eventSink EventSink,
	local io.ReadWriter,
	rawJWT string,
) error {
	ctx = log.Ctx(ctx).With().Str("component", "http3tunneler").Logger().WithContext(ctx)

	eventSink.OnConnecting(ctx)

	transport, err := t.getTransport(false)
	if err != nil {
		return err
	}
	defer func() {
		err := transport.Close()
		if err != nil {
			log.Ctx(ctx).Error().Err(err).Msg("error closing http3 transport")
		}
	}()

	pr, pw := io.Pipe()

	u, err := url.Parse("https://" + t.cfg.proxyHost)
	if err != nil {
		return fmt.Errorf("http/3: failed to parse proxy URL: %w", err)
	}
	hdr := http.Header{}
	if rawJWT != "" {
		hdr.Set("Authorization", "Pomerium "+rawJWT)
	}
	res, err := transport.RoundTrip(&http.Request{
		Method:        http.MethodConnect,
		URL:           u,
		Host:          t.cfg.dstHost,
		Header:        hdr,
		ContentLength: -1,
		Body:          pr,
	})
	if err != nil {
		return fmt.Errorf("http/3: %w: failed to make connect request: %w", errUnsupported, err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusServiceUnavailable:
		return errUnavailable
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		return errUnauthenticated
	default:
		return fmt.Errorf("http/3: invalid response code: %d", res.StatusCode)
	}

	eventSink.OnConnected(ctx)

	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(pw, local)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(local, res.Body)
		errc <- err
	}()

	select {
	case err = <-errc:
	case <-ctx.Done():
		err = context.Cause(ctx)
	}

	eventSink.OnDisconnected(ctx, err)

	return err
}

func (t *http3tunneler) TunnelUDP(
	ctx context.Context,
	eventSink EventSink,
	local UDPPacketReaderWriter,
	rawJWT string,
) error {
	ctx = log.Ctx(ctx).With().Str("component", "http3tunneler").Logger().WithContext(ctx)

	eventSink.OnConnecting(ctx)

	transport, err := t.getTransport(true)
	if err != nil {
		return err
	}
	defer func() {
		err := transport.Close()
		if err != nil {
			log.Ctx(ctx).Error().Err(err).Msg("error closing http3 transport")
		}
	}()

	conn, err := quic.DialAddr(ctx, t.cfg.proxyHost, transport.TLSClientConfig, transport.QUICConfig)
	if err != nil {
		return fmt.Errorf("http/3: failed to connect to server: %w", err)
	}

	cc := transport.NewClientConn(conn)

	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-cc.ReceivedSettings():
	}
	cc.ReceivedSettings()
	settings := cc.Settings()
	if !settings.EnableExtendedConnect {
		return fmt.Errorf("http/3: extended connect not enabled")
	}
	if !settings.EnableDatagrams {
		return fmt.Errorf("http/3: datagrams not enabled")
	}

	rstr, err := cc.OpenRequestStream(ctx)
	if err != nil {
		return fmt.Errorf("http/3: failed to create request stream: %w", err)
	}

	req, err := t.getConnectUDPRequest(ctx, rawJWT)
	if err != nil {
		return err
	}

	err = rstr.SendRequestHeader(req)
	if err != nil {
		return fmt.Errorf("http/3: error sending request: %w", err)
	}

	res, err := rstr.ReadResponse()
	if err != nil {
		return fmt.Errorf("http/3: error reading response: %w", err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusServiceUnavailable:
		return errUnavailable
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		return errUnauthenticated
	default:
		return fmt.Errorf("http/3: invalid http response code: %d", res.StatusCode)
	}

	eventSink.OnConnected(ctx)

	eg, ectx := errgroup.WithContext(ctx)
	eg.Go(func() error { return t.skipCapsules(ectx, rstr) })
	eg.Go(func() error { return t.readLocal(ctx, rstr, local) })
	eg.Go(func() error { return t.readRemote(ctx, local, rstr) })
	err = eg.Wait()

	eventSink.OnDisconnected(ctx, err)

	return err
}

func (t *http3tunneler) getConnectUDPRequest(ctx context.Context, rawJWT string) (*http.Request, error) {
	dstHost, dstPort, err := net.SplitHostPort(t.cfg.dstHost)
	if err != nil {
		return nil, fmt.Errorf("http/3: failed to split destination host into host and port")
	}

	u, err := url.Parse(fmt.Sprintf("https://%s/.well-known/masque/udp/%s/%s/", t.cfg.proxyHost, dstHost, dstPort))
	if err != nil {
		return nil, fmt.Errorf("http/3: failed to create destination url: %w", err)
	}

	capsuleProtocolHeaderValue, err := httpsfv.Marshal(httpsfv.NewItem(true))
	if err != nil {
		return nil, fmt.Errorf("http/3: failed to encode capsule protocol header value")
	}

	hdr := http.Header{
		http3.CapsuleProtocolHeader: {capsuleProtocolHeaderValue},
	}
	if rawJWT != "" {
		hdr.Set("Authorization", "Pomerium "+rawJWT)
	}
	return (&http.Request{
		Method: http.MethodConnect,
		Proto:  "connect-udp",
		Host:   u.Host,
		Header: hdr,
		URL:    u,
	}).WithContext(ctx), nil
}

func (t *http3tunneler) getTransport(enableDatagrams bool) (*http3.Transport, error) {
	cfg := t.cfg.tlsConfig
	if cfg == nil {
		return nil, fmt.Errorf("http/3: %w: TLS is required", errUnsupported)
	}
	cfg = cfg.Clone()
	cfg.NextProtos = []string{http3.NextProtoH3}

	transport := &http3.Transport{
		TLSClientConfig: cfg,
	}
	if enableDatagrams {
		transport.EnableDatagrams = true
		transport.QUICConfig = &quic.Config{
			EnableDatagrams:   true,
			InitialPacketSize: 1350, // use a higher initial packet size so quic itself can be proxied
		}
	}
	return transport, nil
}

func (t *http3tunneler) readLocal(ctx context.Context, dst http3.Stream, src UDPPacketReader) error {
	var logMaxDatagramPayloadSizeOnce sync.Once
	for {
		packet, err := src.ReadPacket(ctx)
		if err != nil {
			return fmt.Errorf("http/3: error reading packet from local udp connection: %w", err)
		}

		data := make([]byte, 0, len(contextIDZero)+len(packet.Payload))
		data = append(data, contextIDZero...)
		data = append(data, packet.Payload...)

		err = dst.SendDatagram(data)

		var tooLargeError *quic.DatagramTooLargeError
		if errors.As(err, &tooLargeError) {
			logMaxDatagramPayloadSizeOnce.Do(func() {
				log.Ctx(ctx).Error().
					Int64("max-datagram-payload-size", tooLargeError.MaxDatagramPayloadSize).
					Int("datagram-size", len(data)).
					Msg("datagram exceeded max datagram payload size and was dropped")
			})
			// ignore
		} else if err != nil {
			return fmt.Errorf("http/3: error sending datagram: %w", err)
		}
	}
}

func (t *http3tunneler) readRemote(ctx context.Context, dst UDPPacketWriter, src http3.Stream) error {
	for {
		data, err := src.ReceiveDatagram(ctx)
		if err != nil {
			return fmt.Errorf("http/3: error reading datagram: %w", err)
		}

		contextID, n, err := quicvarint.Parse(data)
		if err != nil {
			return fmt.Errorf("http/3: error parsing datagram context id: %w", err)
		}

		if contextID != 0 {
			// we only support context-id = 0
			continue
		}

		err = dst.WritePacket(ctx, UDPPacket{
			Payload: data[n:],
		})
		if err != nil {
			return fmt.Errorf("http/3: error writing packet to udp connection: %w", err)
		}
	}
}

func (t *http3tunneler) skipCapsules(ctx context.Context, str http3.Stream) error {
	stop := context.AfterFunc(ctx, func() { str.CancelRead(0) })
	defer stop()
	r := quicvarint.NewReader(str)
	for {
		_, r, err := http3.ParseCapsule(r)
		if errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("error parsing http3 capsule: %w", err)
		}
		_, err = io.Copy(io.Discard, r)
		if err != nil {
			return fmt.Errorf("error reading http3 capsule payload: %w", err)
		}
	}
}
