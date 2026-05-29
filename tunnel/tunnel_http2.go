package tunnel

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/rs/zerolog/log"
	"golang.org/x/net/http2"
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

	hdr := http.Header{}
	if rawJWT != "" {
		hdr.Set("Authorization", "Pomerium "+rawJWT)
	}

	if t.cfg.tlsConfig == nil {
		return fmt.Errorf("%w: http2 requires TLS", errUnsupported)
	}

	proxyURL, err := resolveEdgeProxy(t.cfg)
	if err != nil {
		return fmt.Errorf("http/2: failed to resolve forward proxy: %w", err)
	}
	raw, err := dialEdgeTLS(ctx, t.cfg, proxyURL, []string{"h2"})
	if err != nil {
		return fmt.Errorf("http/2: failed to establish connection to proxy: %w", err)
	}
	defer raw.Close()

	remote, ok := raw.(*tls.Conn)
	if !ok {
		return fmt.Errorf("%w: unexpected connection type returned from dial: %T", errUnsupported, raw)
	}

	protocol := remote.ConnectionState().NegotiatedProtocol
	if protocol != "h2" {
		return fmt.Errorf("%w: unexpected TLS protocol: %s", errUnsupported, protocol)
	}

	cc, err := (&http2.Transport{}).NewClientConn(remote)
	if err != nil {
		return fmt.Errorf("http/2: failed to establish connection: %w", err)
	}
	defer cc.Close()

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

	err = httpStatusCodeToError(res.StatusCode)
	if err != nil {
		return err
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
