// Package tcptunnel contains an implementation of a TCP tunnel via HTTP Connect.
package tcptunnel

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	backoff "github.com/cenkalti/backoff/v4"

	"github.com/pomerium/cli/authclient"
	"github.com/pomerium/cli/jwt"
)

// A Tunnel represents a TCP tunnel over HTTP Connect.
type Tunnel struct {
	cfg  *config
	auth *authclient.AuthClient
}

// New creates a new Tunnel.
func New(options ...Option) *Tunnel {
	cfg := getConfig(options...)
	return &Tunnel{
		cfg: cfg,
		auth: authclient.New(
			authclient.WithBrowserCommand(cfg.browserConfig),
			authclient.WithTLSConfig(cfg.tlsConfig)),
	}
}

// RunListener runs a network listener on the given address. For each
// incoming connection a new TCP tunnel is established via Run.
func (tun *Tunnel) RunListener(ctx context.Context, listenerAddress string) error {
	li, err := net.Listen("tcp", listenerAddress)
	if err != nil {
		return err
	}
	defer func() { _ = li.Close() }()
	log.Println("listening on " + li.Addr().String())

	go func() {
		<-ctx.Done()
		_ = li.Close()
	}()

	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = 0

	for {
		c, err := li.Accept()
		if err != nil {
			// canceled, so ignore the error and return
			if ctx.Err() != nil {
				return nil
			}

			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				log.Printf("temporarily failed to accept local connection: %v\n", err)
				select {
				case <-time.After(bo.NextBackOff()):
				case <-ctx.Done():
					return ctx.Err()
				}
				continue
			}
			return err
		}
		bo.Reset()

		go func(conn net.Conn) {
			defer func() { _ = c.Close() }()

			err := tun.Run(ctx, c, DiscardEvents())
			if err != nil {
				log.Printf("error serving local connection: %v\n", err)
			}
		}(c)
	}
}

// Run establishes a TCP tunnel via HTTP Connect and forwards all traffic from/to local.
func (tun *Tunnel) Run(ctx context.Context, local io.ReadWriter, evt TunnelEvents) error {
	rawJWT, err := tun.cfg.jwtCache.LoadJWT(tun.jwtCacheKey())
	switch {
	// if there is no error, or it is one of the pre-defined cliutil errors,
	// then ignore and use an empty JWT
	case err == nil,
		errors.Is(err, jwt.ErrExpired),
		errors.Is(err, jwt.ErrInvalid),
		errors.Is(err, jwt.ErrNotFound):
	default:
		return fmt.Errorf("tcptunnel: failed to load JWT: %w", err)
	}
	return tun.run(ctx, evt, local, rawJWT, 0)
}

func (tun *Tunnel) run(ctx context.Context, evt TunnelEvents, local io.ReadWriter, rawJWT string, retryCount int) error {
	evt.OnConnecting(ctx)

	hdr := http.Header{}
	if rawJWT != "" {
		hdr.Set("Authorization", "Pomerium "+rawJWT)
	}

	req := (&http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Opaque: tun.cfg.dstHost},
		Host:   tun.cfg.dstHost,
		Header: hdr,
	}).WithContext(ctx)

	var remote net.Conn
	var err error
	if tun.cfg.tlsConfig != nil {
		remote, err = (&tls.Dialer{Config: tun.cfg.tlsConfig}).DialContext(ctx, "tcp", tun.cfg.proxyHost)
	} else {
		remote, err = (&net.Dialer{}).DialContext(ctx, "tcp", tun.cfg.proxyHost)
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
		// don't delete the JWT if we get a service unavailable
		return fmt.Errorf("invalid http response code: %s", res.Status)
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		if retryCount == 0 {
			_ = remote.Close()

			serverURL := &url.URL{
				Scheme: "http",
				Host:   tun.cfg.proxyHost,
			}
			if tun.cfg.tlsConfig != nil {
				serverURL.Scheme = "https"
			}

			rawJWT, err = tun.auth.GetJWT(ctx, serverURL, func(authURL string) { evt.OnAuthRequired(ctx, authURL) })
			if err != nil {
				return fmt.Errorf("failed to get authentication JWT: %w", err)
			}

			err = tun.cfg.jwtCache.StoreJWT(tun.jwtCacheKey(), rawJWT)
			if err != nil {
				return fmt.Errorf("failed to store JWT: %w", err)
			}

			return tun.run(ctx, evt, local, rawJWT, retryCount+1)
		}
		fallthrough
	default:
		_ = tun.cfg.jwtCache.DeleteJWT(tun.jwtCacheKey())
		return fmt.Errorf("invalid http response code: %d", res.StatusCode)
	}

	log.Println("connection established")
	evt.OnConnected(ctx)

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
	case err := <-errc:
		return err
	case <-ctx.Done():
		return nil
	}
}

func (tun *Tunnel) jwtCacheKey() string {
	return fmt.Sprintf("%s|%v", tun.cfg.proxyHost, tun.cfg.tlsConfig != nil)
}

func deBuffer(br *bufio.Reader, underlying io.Reader) io.Reader {
	if br.Buffered() == 0 {
		return underlying
	}
	return io.MultiReader(io.LimitReader(br, int64(br.Buffered())), underlying)
}
