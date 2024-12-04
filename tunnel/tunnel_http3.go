package tunnel

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/quic-go/quic-go/http3"
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
	eventSink.OnConnecting(ctx)

	cfg := t.cfg.tlsConfig
	if cfg == nil {
		return fmt.Errorf("http/3: %w: TLS is required", errUnsupported)
	}
	cfg = cfg.Clone()
	cfg.NextProtos = []string{http3.NextProtoH3}

	transport := (&http3.Transport{
		TLSClientConfig: cfg,
	})
	defer func() {
		transport.Close()
		log.Println("connection closed")
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

	log.Println("http/3: connection established")
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
