// Package authclient contains a CLI authentication client for Pomerium.
package authclient

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// An AuthClient retrieves an authentication JWT via the Pomerium login API.
type AuthClient struct {
	cfg *config
}

// New creates a new AuthClient.
func New(options ...Option) *AuthClient {
	return &AuthClient{
		cfg: getConfig(options...),
	}
}

func (client *AuthClient) CheckBearerToken(ctx context.Context, serverURL *url.URL, bearerToken string) error {
	browserURL := getBrowserURL(serverURL)
	dst := browserURL.ResolveReference(&url.URL{
		Path: "/livez",
	})

	req, err := http.NewRequest("GET", dst.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	_, err = client.fetch(ctx, req)
	return err
}

// GetJWT retrieves a JWT from Pomerium.
func (client *AuthClient) GetJWT(ctx context.Context, serverURL *url.URL, onOpenBrowser func(string)) (rawJWT string, err error) {
	if client.cfg.serviceAccount != "" {
		return client.cfg.serviceAccount, nil
	}

	if client.cfg.serviceAccountFile != "" {
		rawJWTBytes, err := os.ReadFile(client.cfg.serviceAccountFile)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(rawJWTBytes)), nil
	}

	li, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("failed to start listener: %w", err)
	}
	defer func() { _ = li.Close() }()

	incomingJWT := make(chan string)
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return client.runHTTPServer(ctx, li, incomingJWT)
	})
	eg.Go(func() error {
		return client.runOpenBrowser(ctx, li, serverURL, onOpenBrowser)
	})
	eg.Go(func() error {
		select {
		case rawJWT = <-incomingJWT:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	err = eg.Wait()
	if err != nil {
		return "", err
	}

	return rawJWT, nil
}

func (client *AuthClient) runHTTPServer(ctx context.Context, li net.Listener, incomingJWT chan string) error {
	var srv *http.Server
	srv = &http.Server{
		BaseContext: func(li net.Listener) context.Context {
			return ctx
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			jwt := r.FormValue("pomerium_jwt")
			if jwt == "" {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			incomingJWT <- jwt

			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "login complete, you may close this page")

			go func() { _ = srv.Shutdown(ctx) }()
		}),
	}
	// shutdown the server when ctx is done.
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(ctx)
	}()
	err := srv.Serve(li)
	if err == http.ErrServerClosed {
		err = nil
	}
	return err
}

func (client *AuthClient) runOpenBrowser(ctx context.Context, li net.Listener, serverURL *url.URL, onOpenBrowser func(string)) error {
	browserURL := getBrowserURL(serverURL)
	dst := browserURL.ResolveReference(&url.URL{
		Path: "/.pomerium/api/v1/login",
		RawQuery: url.Values{
			"pomerium_redirect_uri": {fmt.Sprintf("http://%s", li.Addr().String())},
		}.Encode(),
	})

	req, err := http.NewRequest("GET", dst.String(), nil)
	if err != nil {
		return err
	}

	bs, err := client.fetch(ctx, req)
	if err != nil {
		return err
	}

	onOpenBrowser(string(bs))
	err = client.cfg.open(string(bs))
	if err != nil {
		return fmt.Errorf("failed to open browser url: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stderr, "Your browser has been opened to visit:\n\n%s\n\n", string(bs))
	return nil
}

func (client *AuthClient) fetch(ctx context.Context, req *http.Request) ([]byte, error) {
	ctx, clearTimeout := context.WithTimeout(ctx, 10*time.Second)
	defer clearTimeout()
	req = req.WithContext(ctx)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = client.cfg.tlsConfig

	hc := &http.Client{
		Transport: transport,
	}

	res, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get url: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("unexpected status code: %s", res.Status)
	}

	return io.ReadAll(res.Body)
}

func getBrowserURL(serverURL *url.URL) *url.URL {
	browserURL := new(url.URL)
	*browserURL = *serverURL

	// remove unnecessary ports to avoid HMAC error
	if browserURL.Scheme == "http" && browserURL.Host == browserURL.Hostname()+":80" {
		browserURL.Host = browserURL.Hostname()
	} else if browserURL.Scheme == "https" && browserURL.Host == browserURL.Hostname()+":443" {
		browserURL.Host = browserURL.Hostname()
	}

	return browserURL
}
