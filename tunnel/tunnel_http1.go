package tunnel

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
)

type http1tunnel struct {
	cfg *config
}

func (t *http1tunnel) TunnelTCP(
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

	req := (&http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Opaque: t.cfg.dstHost},
		Host:   t.cfg.dstHost,
		Header: hdr,
	}).WithContext(ctx)

	var remote net.Conn
	var err error
	if t.cfg.tlsConfig != nil {
		remote, err = (&tls.Dialer{Config: t.cfg.tlsConfig}).DialContext(ctx, "tcp", t.cfg.proxyHost)
	} else {
		remote, err = (&net.Dialer{}).DialContext(ctx, "tcp", t.cfg.proxyHost)
	}
	if err != nil {
		return fmt.Errorf("failed to establish connection to proxy: %w", err)
	}
	defer func() {
		_ = remote.Close()
		log.Println("connection closed")
	}()
	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			_ = remote.Close()
		}()
	}

	err = req.Write(remote)
	if err != nil {
		return err
	}

	br := bufio.NewReader(remote)
	res, err := http.ReadResponse(br, req)
	if err != nil {
		return fmt.Errorf("failed to read HTTP response: %w", err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

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

	log.Println("connection established")
	eventSink.OnConnected(ctx)

	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(remote, local)
		errc <- err
	}()
	remoteReader := deBuffer(br, remote)
	go func() {
		_, err := io.Copy(local, remoteReader)
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

func deBuffer(br *bufio.Reader, underlying io.Reader) io.Reader {
	if br.Buffered() == 0 {
		return underlying
	}
	return io.MultiReader(io.LimitReader(br, int64(br.Buffered())), underlying)
}
