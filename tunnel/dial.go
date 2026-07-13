package tunnel

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"

	"github.com/pomerium/cli/internal/httputil"
)

// edgeURL builds the URL of the Pomerium edge for proxy resolution.
func edgeURL(cfg *config) *url.URL {
	scheme := "http"
	if cfg.tlsConfig != nil {
		scheme = "https"
	}
	return &url.URL{Scheme: scheme, Host: cfg.proxyHost}
}

// resolveEdgeProxy resolves the forward proxy (if any) for reaching the edge.
// Forward proxying is TCP-only; UDP callers must not use it.
func resolveEdgeProxy(cfg *config) (*url.URL, error) {
	return httputil.ResolveProxy(cfg.forwardProxy, edgeURL(cfg))
}

// dialEdgeTLS establishes a connection to the Pomerium edge, routing through
// proxyURL when non-nil, and TLS-wraps it when a tls config is configured.
// The config's ALPN is already pinned to http/1.1 by WithTLSConfig.
func dialEdgeTLS(ctx context.Context, cfg *config, proxyURL *url.URL) (net.Conn, error) {
	var raw net.Conn
	var err error
	if proxyURL == nil {
		raw, err = (&net.Dialer{}).DialContext(ctx, "tcp", cfg.proxyHost)
	} else {
		raw, err = httputil.DialThroughProxy(ctx, proxyURL, cfg.proxyHost)
	}
	if err != nil {
		return nil, err
	}

	if cfg.tlsConfig == nil {
		return raw, nil
	}

	tlsCfg := cfg.tlsConfig.Clone()
	// tls.Client does not derive ServerName from the dial address; set it or
	// certificate verification fails.
	if tlsCfg.ServerName == "" {
		host, _, err := net.SplitHostPort(cfg.proxyHost)
		if err != nil {
			host = cfg.proxyHost
		}
		tlsCfg.ServerName = host
	}

	tc := tls.Client(raw, tlsCfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return tc, nil
}
