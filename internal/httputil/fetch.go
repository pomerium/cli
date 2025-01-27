package httputil

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrUnauthenticated indicates the user needs to authenticate.
var ErrUnauthenticated = errors.New("unauthenticated")

// Fetch fetches the http request.
func Fetch(ctx context.Context, tlsConfig *tls.Config, req *http.Request) ([]byte, error) {
	ctx, clearTimeout := context.WithTimeout(ctx, 10*time.Second)
	defer clearTimeout()
	req = req.WithContext(ctx)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	hc := &http.Client{
		Transport: transport,
	}

	res, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get url: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

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
