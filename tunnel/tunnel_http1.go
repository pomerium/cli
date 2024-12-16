package tunnel

import (
	"bufio"
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
	"golang.org/x/sync/errgroup"
)

type http1tunneler struct {
	cfg *config
}

func (t *http1tunneler) TunnelTCP(
	ctx context.Context,
	eventSink EventSink,
	local io.ReadWriter,
	rawJWT string,
) error {
	ctx = log.Ctx(ctx).With().Str("component", "http1tunneler").Logger().WithContext(ctx)

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
		return fmt.Errorf("http/1: failed to establish connection to proxy: %w", err)
	}
	defer func() {
		_ = remote.Close()
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
		return fmt.Errorf("http/1: failed to read HTTP response: %w", err)
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
		return fmt.Errorf("http/1: invalid http response code: %d", res.StatusCode)
	}

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

func (t *http1tunneler) TunnelUDP(
	ctx context.Context,
	eventSink EventSink,
	local UDPDatagramReaderWriter,
	rawJWT string,
) error {
	eventSink.OnConnecting(ctx)

	var remote net.Conn
	var err error
	if t.cfg.tlsConfig != nil {
		remote, err = (&tls.Dialer{Config: t.cfg.tlsConfig}).DialContext(ctx, "tcp", t.cfg.proxyHost)
	} else {
		remote, err = (&net.Dialer{}).DialContext(ctx, "tcp", t.cfg.proxyHost)
	}
	if err != nil {
		return fmt.Errorf("http/1: failed to establish connection to proxy: %w", err)
	}
	defer func() { _ = remote.Close() }()
	context.AfterFunc(ctx, func() { _ = remote.Close() })

	dstHost, dstPort, err := net.SplitHostPort(t.cfg.dstHost)
	if err != nil {
		return fmt.Errorf("http/1: failed to split destination host into host and port")
	}

	u, err := url.Parse(fmt.Sprintf("https://%s/.well-known/masque/udp/%s/%s/", t.cfg.proxyHost, dstHost, dstPort))
	if err != nil {
		return fmt.Errorf("http/1: failed to create destination url: %w", err)
	}

	capsuleProtocolHeaderValue, err := httpsfv.Marshal(httpsfv.NewItem(true))
	if err != nil {
		return fmt.Errorf("http/1: failed to encode capsule protocol header value")
	}

	hdr := http.Header{
		"Connection":                {"Upgrade"},
		"Upgrade":                   {"connect-udp"},
		http3.CapsuleProtocolHeader: {capsuleProtocolHeaderValue},
	}
	if rawJWT != "" {
		hdr.Set("Authorization", "Pomerium "+rawJWT)
	}
	req := (&http.Request{
		Method: http.MethodGet,
		URL:    u,
		Host:   t.cfg.dstHost,
		Header: hdr,
	}).WithContext(ctx)

	err = req.Write(remote)
	if err != nil {
		return err
	}

	br := bufio.NewReader(remote)
	res, err := http.ReadResponse(br, req)
	if err != nil {
		return fmt.Errorf("http/1: failed to read HTTP response: %w", err)
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
		return fmt.Errorf("http/1: invalid http response code: %d", res.StatusCode)
	}

	eventSink.OnConnected(ctx)

	eg, ectx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return streamFromCapsuleDatagramsToUDPDatagramWriter(ectx, local, res.Body)
	})
	eg.Go(func() error {
		return streamFromUDPDatagramReaderToCapsuleDatagrams(ectx, remote, local)
	})
	err = eg.Wait()

	eventSink.OnDisconnected(ctx, err)

	return err
}

func deBuffer(br *bufio.Reader, underlying io.Reader) io.Reader {
	if br.Buffered() == 0 {
		return underlying
	}
	return io.MultiReader(io.LimitReader(br, int64(br.Buffered())), underlying)
}
