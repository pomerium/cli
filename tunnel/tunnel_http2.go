package tunnel

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"github.com/dunglas/httpsfv"
	"github.com/quic-go/quic-go/http3"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/http2"
	"golang.org/x/sync/errgroup"
)

type http2tunneler struct {
	cfg *config
}

func (t *http2tunneler) TunnelTCP(
	ctx context.Context,
	eventSink EventSink,
	local io.ReadWriter,
	rawJWT string,
) error {
	ctx = log.Ctx(ctx).With().Str("component", "http2tunneler").Logger().WithContext(ctx)

	eventSink.OnConnecting(ctx)

	cc, err := t.getClientConn(ctx)
	if err != nil {
		return err
	}
	defer cc.Close()

	hdr := http.Header{}
	if rawJWT != "" {
		hdr.Set("Authorization", "Pomerium "+rawJWT)
	}

	pr, pw := io.Pipe()

	req := (&http.Request{
		Method:        "CONNECT",
		URL:           &url.URL{Opaque: t.cfg.dstHost},
		Host:          t.cfg.dstHost,
		Header:        hdr,
		Body:          pr,
		ContentLength: -1,
	}).WithContext(ctx)

	res, err := cc.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("http/2: error making connect request: %w", err)
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
		return fmt.Errorf("http/2: invalid http response code: %d", res.StatusCode)
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

func (t *http2tunneler) TunnelUDP(
	ctx context.Context,
	eventSink EventSink,
	local UDPPacketReaderWriter,
	rawJWT string,
) error {
	ctx = log.Ctx(ctx).With().Str("component", "http2tunneler").Logger().WithContext(ctx)

	eventSink.OnConnecting(ctx)

	cc, err := t.getClientConn(ctx)
	if err != nil {
		return err
	}
	defer cc.Close()

	pr, pw := io.Pipe()

	req, err := buildExtendedConnectUDPRequest(ctx, t.cfg.proxyHost, t.cfg.dstHost, rawJWT, pr)
	if err != nil {
		return fmt.Errorf("http/2: failed to build extended connect udp request: %w", err)
	}

	res, err := cc.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("http/2: error making connect udp request: %w", err)
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
		return fmt.Errorf("http/2: invalid http response code: %d", res.StatusCode)
	}

	eventSink.OnConnected(ctx)

	eg, ectx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return streamFromCapsuleDatagramsToUDPPacketWriter(ectx, local, res.Body)
	})
	eg.Go(func() error {
		return streamFromUDPPacketReaderToCapsuleDatagrams(ectx, pw, local)
	})
	err = eg.Wait()

	eventSink.OnDisconnected(ctx, err)

	return err
}

func (t *http2tunneler) getClientConn(
	ctx context.Context,
) (*http2.ClientConn, error) {
	if t.cfg.tlsConfig == nil {
		return nil, fmt.Errorf("%w: http2 requires TLS", errUnsupported)
	}
	cfg := t.cfg.tlsConfig.Clone()
	cfg.NextProtos = []string{"h2"}

	raw, err := (&tls.Dialer{Config: cfg}).DialContext(ctx, "tcp", t.cfg.proxyHost)
	if err != nil {
		return nil, fmt.Errorf("http/2: failed to establish connection to proxy: %w", err)
	}
	defer func() { _ = raw.Close() }()

	remote, ok := raw.(*tls.Conn)
	if !ok {
		return nil, fmt.Errorf("http/2: unexpected connection type returned from dial: %T", raw)
	}

	protocol := remote.ConnectionState().NegotiatedProtocol
	if protocol != "h2" {
		return nil, fmt.Errorf("%w: unexpected TLS protocol: %s", errUnsupported, protocol)
	}

	cc, err := (&http2.Transport{}).NewClientConn(remote)
	if err != nil {
		return nil, fmt.Errorf("http/2: failed to establish connection: %w", err)
	}

	return cc, nil
}

func buildExtendedConnectUDPRequest(
	ctx context.Context,
	proxyHost, dstHost string,
	rawJWT string,
	body io.ReadCloser,
) (*http.Request, error) {
	capsuleProtocolHeaderValue, err := httpsfv.Marshal(httpsfv.NewItem(true))
	if err != nil {
		return nil, fmt.Errorf("failed to encode capsule protocol header value")
	}

	hdr := http.Header{
		":protocol":                 {"connect-udp"},
		":scheme":                   {"https"},
		http3.CapsuleProtocolHeader: {capsuleProtocolHeaderValue},
	}
	if rawJWT != "" {
		hdr.Set("Authorization", "Pomerium "+rawJWT)
	}
	dstHost, dstPort, err := net.SplitHostPort(dstHost)
	if err != nil {
		return nil, fmt.Errorf("failed to split destination host into host and port")
	}
	u, err := url.Parse(fmt.Sprintf("https://%s/.well-known/masque/udp/%s/%s/", proxyHost, dstHost, dstPort))
	if err != nil {
		return nil, fmt.Errorf("failed to create destination url: %w", err)
	}
	return (&http.Request{
		Method:        http.MethodConnect,
		URL:           u,
		Host:          proxyHost,
		Header:        hdr,
		ContentLength: -1,
		Body:          body,
	}).WithContext(ctx), nil
}
