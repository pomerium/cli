// Package authclient contains a CLI authentication client for Pomerium.
package authclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pomerium/pomerium/pkg/identity/oidc"
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

	if client.cfg.deviceCodeFlow {
		return client.runDeviceCodeFlow(ctx, serverURL)
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
	browserURL := new(url.URL)
	*browserURL = *serverURL

	// remove unnecessary ports to avoid HMAC error
	if browserURL.Scheme == "http" && browserURL.Host == browserURL.Hostname()+":80" {
		browserURL.Host = browserURL.Hostname()
	} else if browserURL.Scheme == "https" && browserURL.Host == browserURL.Hostname()+":443" {
		browserURL.Host = browserURL.Hostname()
	}

	dst := browserURL.ResolveReference(&url.URL{
		Path: "/.pomerium/api/v1/login",
		RawQuery: url.Values{
			"pomerium_redirect_uri": {fmt.Sprintf("http://%s", li.Addr().String())},
		}.Encode(),
	})

	ctx, clearTimeout := context.WithTimeout(ctx, 10*time.Second)
	defer clearTimeout()

	req, err := http.NewRequestWithContext(ctx, "GET", dst.String(), nil)
	if err != nil {
		return err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = client.cfg.tlsConfig

	hc := &http.Client{
		Transport: transport,
	}

	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get login url: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode/100 != 2 {
		return fmt.Errorf("failed to get login url: %s", res.Status)
	}

	bs, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read login url: %w", err)
	}

	onOpenBrowser(string(bs))
	err = client.cfg.open(string(bs))
	if err != nil {
		return fmt.Errorf("failed to open browser url: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stderr, "Your browser has been opened to visit:\n\n%s\n\n", string(bs))
	return nil
}

type DeviceAuthTokenResponse struct {
	Token string `json:"token"`
}

func (client *AuthClient) runDeviceCodeFlow(ctx context.Context, requestURL *url.URL) (string, error) {
	apiUrl := requestURL.ResolveReference(&url.URL{
		Path: "/.pomerium/api/v1/device_auth",
		RawQuery: url.Values{
			"pomerium_device_auth_route_uri": {requestURL.String()},
		}.Encode(),
	})

	req, err := http.NewRequestWithContext(ctx, "GET", apiUrl.String(), nil)
	if err != nil {
		return "", err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = client.cfg.tlsConfig

	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", err
	}
	hc := &http.Client{
		Timeout:   10 * time.Minute,
		Transport: transport,
		Jar:       jar,
	}

	res, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("authentication failed: %s", res.Status)
	}

	if res.Header.Get("Content-Type") != "application/json" {
		return "", fmt.Errorf("unexpected content type: %s", res.Header.Get("Content-Type"))
	}

	bytes, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	var response oidc.UserDeviceAuthResponse
	if err := json.Unmarshal(bytes, &response); err != nil {
		return "", err
	}

	fmt.Fprintf(os.Stderr, "Authenticate with your browser at %s\n", response.VerificationURIComplete)

	delay := time.Duration(response.InitialRetryDelay) * time.Second
	numRetries := 10
	for i := 0; i < numRetries; i++ {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
		req, err = http.NewRequestWithContext(ctx, "POST", apiUrl.String(), strings.NewReader(url.Values{
			"pomerium_device_auth_retry_token": {base64.URLEncoding.EncodeToString(response.RetryToken)},
		}.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		res, err = hc.Do(req)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()

		switch res.StatusCode {
		case http.StatusOK:
		case http.StatusTooManyRequests:
			if retryAfter := res.Header.Get("Retry-After"); retryAfter != "" {
				if d, err := time.ParseDuration(retryAfter); err == nil {
					delay = d
				}
			}
			continue
		default:
			return "", fmt.Errorf("authentication failed: %s", res.Status)
		}

		if res.Header.Get("Content-Type") != "application/json" {
			return "", fmt.Errorf("unexpected content type: %s", res.Header.Get("Content-Type"))
		}

		tokenBytes, err := io.ReadAll(res.Body)
		if err != nil {
			return "", err
		}

		var tokenResponse DeviceAuthTokenResponse
		if err := json.Unmarshal(tokenBytes, &tokenResponse); err != nil {
			return "", err
		}

		return tokenResponse.Token, nil
	}

	return "", fmt.Errorf("authentication timed out after %d retries", numRetries)
}
