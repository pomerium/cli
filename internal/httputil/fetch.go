package httputil

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// ErrUnauthenticated indicates the user needs to authenticate.
var ErrUnauthenticated = errors.New("unauthenticated")

type fetchConfig struct {
	proxyURL *url.URL
}

// A FetchOption modifies the behavior of Fetch.
type FetchOption func(*fetchConfig)

// WithProxyURL routes the request through the given forward proxy. When unset,
// the default transport still honors HTTP_PROXY/HTTPS_PROXY.
func WithProxyURL(u *url.URL) FetchOption {
	return func(c *fetchConfig) {
		c.proxyURL = u
	}
}

// Fetch fetches the http request.
func Fetch(ctx context.Context, tlsConfig *tls.Config, req *http.Request, opts ...FetchOption) ([]byte, error) {
	var cfg fetchConfig
	for _, o := range opts {
		o(&cfg)
	}

	ctx, clearTimeout := context.WithTimeout(ctx, 10*time.Second)
	defer clearTimeout()
	req = req.WithContext(ctx)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	if cfg.proxyURL != nil {
		// Dial the proxy ourselves so the proxy hop's TLS is validated against
		// system trust (matching the tunnel path), while the target's TLS still
		// uses tlsConfig. Setting transport.Proxy would instead validate an https
		// proxy against tlsConfig and re-read the proxy environment.
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
			return DialThroughProxy(ctx, cfg.proxyURL, addr)
		}
	}
	hc := &http.Client{
		Transport: transport,
	}

	res, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get url: %w", err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusUnauthorized,
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		return nil, fmt.Errorf("%w: unexpected status code: %d", ErrUnauthenticated, res.StatusCode)
	}

	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("unexpected status code: %s", res.Status)
	}

	return io.ReadAll(res.Body)
}
