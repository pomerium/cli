package tunnel

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	"golang.org/x/net/http2"
)

type http2tunnel struct {
	cfg *config
}

func (t *http2tunnel) TunnelTCP(
	ctx context.Context,
	eventSink EventSink,
	local io.ReadWriter,
	rawJWT string,
) error {
	eventSink.OnConnecting(ctx)

	hdr := http.Header{}
	if rawJWT != "" {
		hdr.Set("Authorization", "Pomerium "+rawJWT)
	}

	if t.cfg.tlsConfig == nil {
		return fmt.Errorf("%w: http2 requires TLS", errUnsupported)
	}

	cfg := t.cfg.tlsConfig.Clone()
	cfg.NextProtos = []string{"h2"}

	raw, err := (&tls.Dialer{Config: cfg}).DialContext(ctx, "tcp", t.cfg.proxyHost)
	if err != nil {
		return fmt.Errorf("failed to establish connection to proxy: %w", err)
	}
	defer func() {
		_ = raw.Close()
		log.Println("connection closed")
	}()

	remote, ok := raw.(*tls.Conn)
	if !ok {
		return fmt.Errorf("unexpected connection type returned from dial: %T", raw)
	}

	protocol := remote.ConnectionState().NegotiatedProtocol
	if protocol != "h2" {
		return fmt.Errorf("%w: unexpected TLS protocol: %s", errUnsupported, protocol)
	}

	cc, err := (&http2.Transport{}).NewClientConn(remote)
	if err != nil {
		return fmt.Errorf("failed to establish http2 connection: %w", err)
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
		return fmt.Errorf("error making http2 connect request: %w", err)
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
		return fmt.Errorf("invalid http response code: %d", res.StatusCode)
	}

	log.Println("connection established via http2")
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
