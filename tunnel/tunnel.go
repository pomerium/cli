// Package tunnel contains an implementation of a TCP tunnel via HTTP Connect.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"time"

	backoff "github.com/cenkalti/backoff/v4"

	"github.com/pomerium/cli/authclient"
	"github.com/pomerium/cli/jwt"
)

var (
	errUnavailable     = errors.New("unavailable")
	errUnauthenticated = errors.New("unauthenticated")
	errUnsupported     = errors.New("unsupported")
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
			authclient.WithServiceAccount(cfg.serviceAccount),
			authclient.WithServiceAccountFile(cfg.serviceAccountFile),
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
func (tun *Tunnel) Run(ctx context.Context, local io.ReadWriter, eventSink EventSink) error {
	rawJWT, err := tun.cfg.jwtCache.LoadJWT(tun.jwtCacheKey())
	switch {
	// if there is no error, or it is one of the pre-defined cliutil errors,
	// then ignore and use an empty JWT
	case err == nil,
		errors.Is(err, jwt.ErrExpired),
		errors.Is(err, jwt.ErrInvalid),
		errors.Is(err, jwt.ErrNotFound):
	default:
		return fmt.Errorf("tunnel: failed to load JWT: %w", err)
	}
	return tun.run(ctx, eventSink, local, rawJWT, 0)
}

func (tun *Tunnel) run(ctx context.Context, eventSink EventSink, local io.ReadWriter, rawJWT string, retryCount int) error {
	err := (&http2tunnel{cfg: tun.cfg}).TunnelTCP(ctx, eventSink, local, rawJWT)
	if errors.Is(err, errUnsupported) {
		// fallback to http1
		err = (&http1tunnel{cfg: tun.cfg}).TunnelTCP(ctx, eventSink, local, rawJWT)
	}

	if errors.Is(err, errUnavailable) {
		// don't delete the JWT if we get a service unavailable
		return err
	} else if errors.Is(err, errUnauthenticated) && retryCount == 0 {
		serverURL := &url.URL{
			Scheme: "http",
			Host:   tun.cfg.proxyHost,
		}
		if tun.cfg.tlsConfig != nil {
			serverURL.Scheme = "https"
		}

		rawJWT, err = tun.auth.GetJWT(ctx, serverURL, func(authURL string) {
			eventSink.OnAuthRequired(ctx, authURL)
		})
		if err != nil {
			return fmt.Errorf("failed to get authentication JWT: %w", err)
		}

		err = tun.cfg.jwtCache.StoreJWT(tun.jwtCacheKey(), rawJWT)
		if err != nil {
			return fmt.Errorf("failed to store JWT: %w", err)
		}

		return tun.run(ctx, eventSink, local, rawJWT, retryCount+1)
	} else if err != nil {
		_ = tun.cfg.jwtCache.DeleteJWT(tun.jwtCacheKey())
		return err
	}

	return nil
}

func (tun *Tunnel) jwtCacheKey() string {
	return fmt.Sprintf("%s|%v", tun.cfg.proxyHost, tun.cfg.tlsConfig != nil)
}