package httputil

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/proxy"
)

// ResolveProxy determines the forward proxy to use to reach edge, if any.
//
// Precedence: an explicit override (authoritative, ignores NO_PROXY), then
// HTTP_PROXY/HTTPS_PROXY honoring NO_PROXY (scheme-aware), then ALL_PROXY as a
// fallback. It returns (nil, nil) when no proxy applies.
func ResolveProxy(override string, edge *url.URL) (*url.URL, error) {
	if override = strings.TrimSpace(override); override != "" {
		return normalizeForwardProxy(override)
	}

	if u, err := httpproxy.FromEnvironment().ProxyFunc()(edge); err != nil {
		return nil, err
	} else if u != nil {
		return u, nil
	}

	all := os.Getenv("ALL_PROXY")
	if all == "" {
		all = os.Getenv("all_proxy")
	}
	if all == "" {
		return nil, nil
	}
	noProxy := os.Getenv("NO_PROXY")
	if noProxy == "" {
		noProxy = os.Getenv("no_proxy")
	}
	return (&httpproxy.Config{HTTPProxy: all, HTTPSProxy: all, NoProxy: noProxy}).ProxyFunc()(edge)
}

// ProxyFetchOptions resolves the forward proxy for target and returns the
// Fetch options that route requests through it. Plain-http targets with an
// environment-resolved proxy return no options: the default transport already
// proxies them in absolute form, which proxy ACLs commonly allow where a
// CONNECT to port 80 is denied. That carve-out keeps the default transport's
// env semantics for http targets (no ALL_PROXY, proxy TLS verified against
// the request's tls config). An explicit override always applies.
func ProxyFetchOptions(override string, target *url.URL) ([]FetchOption, error) {
	if strings.TrimSpace(override) == "" && target.Scheme != "https" {
		return nil, nil
	}
	proxyURL, err := ResolveProxy(override, target)
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		return nil, nil
	}
	return []FetchOption{WithProxyURL(proxyURL)}, nil
}

// ValidateForwardProxyFlag validates an explicit --forward-proxy value without
// consulting the environment. An empty value is valid and returns (nil, nil);
// the caller then falls back to environment-based resolution at request time.
func ValidateForwardProxyFlag(raw string) (*url.URL, error) {
	if raw = strings.TrimSpace(raw); raw == "" {
		return nil, nil
	}
	return normalizeForwardProxy(raw)
}

// normalizeForwardProxy validates and normalizes an explicit --forward-proxy
// override: a bare host:port defaults to http, the scheme must be one we can
// dial, and a host is required.
func normalizeForwardProxy(override string) (*url.URL, error) {
	if !strings.Contains(override, "://") {
		override = "http://" + override
	}
	u, err := url.Parse(override)
	if err != nil {
		// url.Error echoes the raw URL and url.EscapeError echoes the bytes
		// around a bad escape; either may carry credentials, so surface only a
		// credential-free cause.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		if _, ok := errors.AsType[url.EscapeError](err); ok {
			err = errors.New("invalid percent-escape")
		}
		return nil, fmt.Errorf("invalid forward proxy: %w", err)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("unsupported forward proxy scheme %q (want http, https, socks5, or socks5h)", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("forward proxy %q has no host", u.Redacted())
	}
	// A lone trailing slash (http://proxy:3128/) is harmless; normalize it away.
	if u.Path == "/" {
		u.Path = ""
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("forward proxy %q must not have a path, query, or fragment", u.Redacted())
	}
	return u, nil
}

// proxyDialTimeout bounds the proxy connect and CONNECT exchange when the
// caller's context carries no deadline, so a dead proxy fails fast.
const proxyDialTimeout = 30 * time.Second

// DialThroughProxy dials target (host:port) through the given forward proxy.
func DialThroughProxy(ctx context.Context, proxyURL *url.URL, target string) (net.Conn, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, proxyDialTimeout)
		defer cancel()
	}
	switch proxyURL.Scheme {
	case "socks5", "socks5h":
		d, err := proxy.FromURL(proxyURL, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("failed to create socks5 dialer: %w", err)
		}
		cd, ok := d.(proxy.ContextDialer)
		if !ok {
			return nil, errors.New("socks5 dialer does not support context cancellation")
		}
		return cd.DialContext(ctx, "tcp", target)
	case "http", "https", "":
		return dialHTTPConnect(ctx, proxyURL, target)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %q", proxyURL.Scheme)
	}
}

func dialHTTPConnect(ctx context.Context, proxyURL *url.URL, target string) (_ net.Conn, retErr error) {
	proxyAddr := proxyURL.Host
	if proxyURL.Port() == "" {
		if proxyURL.Scheme == "https" {
			proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "443")
		} else {
			proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "80")
		}
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial forward proxy: %w", err)
	}
	defer func() {
		if retErr != nil {
			_ = conn.Close()
		}
	}()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}

	if proxyURL.Scheme == "https" {
		tc := tls.Client(conn, &tls.Config{ServerName: proxyURL.Hostname()})
		if err := tc.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("forward proxy %s TLS handshake failed (only system-trusted CAs are supported): %w", proxyURL.Redacted(), err)
		}
		conn = tc
	}

	// abort the CONNECT exchange on cancellation; stop() detaches the conn from
	// ctx before it is handed to the caller.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: target},
		Host:   target,
		Header: http.Header{},
	}
	if proxyURL.User != nil {
		user := proxyURL.User.Username()
		pass, _ := proxyURL.User.Password()
		auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		req.Header.Set("Proxy-Authorization", "Basic "+auth)
	}
	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("failed to write CONNECT request: %w", err)
	}

	br := bufio.NewReader(conn)
	res, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, fmt.Errorf("failed to read CONNECT response: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("forward proxy CONNECT failed: %s", res.Status)
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}

	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, br: br}, nil
	}
	return conn, nil
}

// bufferedConn drains bytes the CONNECT response parser buffered before reading
// from the underlying connection. The bufio.Reader transparently falls through
// to the underlying conn once its buffer is exhausted.
type bufferedConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.br.Read(p)
}
