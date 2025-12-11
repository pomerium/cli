// Package tunnel contains an implementation of a TCP tunnel via HTTP Connect.
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
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/rs/zerolog/log"

	"github.com/pomerium/cli/authclient"
	"github.com/pomerium/cli/jwt"
)

var (
	errUnavailable     = errors.New("unavailable")
	errUnauthenticated = errors.New("unauthenticated")
	errUnauthorized    = errors.New("unauthorized")
	errUnsupported     = errors.New("unsupported")
)

// A Tunnel represents a TCP tunnel over HTTP Connect.
type Tunnel struct {
	cfg  *config
	auth *authclient.AuthClient

	mu          sync.Mutex
	tcpTunneler TCPTunneler
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
	ctx = log.Ctx(ctx).With().Str("component", "tunnel").Logger().WithContext(ctx)

	li, err := net.Listen("tcp", listenerAddress)
	if err != nil {
		return err
	}
	defer li.Close()
	log.Ctx(ctx).Info().Str("addr", li.Addr().String()).Msg("started tcp listener")

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
				log.Ctx(ctx).Error().Err(err).Msg("temporarily failed to accept local connection")
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
			defer conn.Close()

			err := tun.Run(ctx, conn, LogEvents())
			if err != nil {
				log.Ctx(ctx).Error().Err(err).Msg("error serving local connection")
			}
		}(c)
	}
}

// Run establishes a TCP tunnel via HTTP Connect and forwards all traffic from/to local.
func (tun *Tunnel) Run(ctx context.Context, local io.ReadWriter, eventSink EventSink) error {
	return tun.runWithJWT(ctx, eventSink, func(ctx context.Context, rawJWT string) error {
		tun.mu.Lock()
		if tun.tcpTunneler == nil {
			tun.tcpTunneler = tun.pickTCPTunneler(ctx)
		}
		tun.mu.Unlock()

		return tun.tcpTunneler.TunnelTCP(ctx, eventSink, local, rawJWT)
	})
}

func (tun *Tunnel) runWithJWT(ctx context.Context, eventSink EventSink, handler func(ctx context.Context, rawJWT string) error) error {
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

	err = handler(ctx, rawJWT)
	if errors.Is(err, errUnauthenticated) {
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
			return fmt.Errorf("tunnel: failed to get authentication JWT: %w", err)
		}

		err = tun.cfg.jwtCache.StoreJWT(tun.jwtCacheKey(), rawJWT)
		if err != nil {
			return fmt.Errorf("tunnel: failed to store JWT: %w", err)
		}

		err = handler(ctx, rawJWT)
	}

	if errors.Is(err, errUnavailable) || errors.Is(err, errUnauthorized) {
		// don't delete the JWT if we get a service unavailable or the user is unauthorized
		return err
	} else if err != nil {
		_ = tun.cfg.jwtCache.DeleteJWT(tun.jwtCacheKey())
		return err
	}

	return nil
}

func (tun *Tunnel) jwtCacheKey() string {
	return jwt.CacheKeyForHost(tun.cfg.proxyHost, tun.cfg.tlsConfig)
}

func httpStatusCodeToError(statusCode int) error {
	switch statusCode {
	case http.StatusOK:
		return nil
	case http.StatusServiceUnavailable:
		return errUnavailable
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		return errUnauthenticated
	case http.StatusForbidden:
		return errUnauthorized
	}

	return fmt.Errorf("invalid http response code: %d", statusCode)
}
