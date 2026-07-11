// Package authclient contains a CLI authentication client for Pomerium.
package authclient

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/pomerium/cli/internal/httputil"
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

// CheckBearerToken checks that a bearer token is valid.
func (client *AuthClient) CheckBearerToken(ctx context.Context, serverURL *url.URL, bearerToken string) error {
	browserURL := getBrowserURL(serverURL)
	dst := browserURL.ResolveReference(&url.URL{
		Path: "/healthz",
	})

	req, err := http.NewRequest("GET", dst.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	_, err = httputil.Fetch(ctx, client.cfg.tlsConfig, req)
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

	return client.getBrowserJWT(ctx, serverURL, nil, onOpenBrowser)
}

// GetBrowserJWT retrieves a new JWT through the interactive browser flow. It
// deliberately ignores configured service accounts and does not consult a
// token cache.
func (client *AuthClient) GetBrowserJWT(
	ctx context.Context,
	serverURL *url.URL,
	loginParams url.Values,
	onOpenBrowser func(string),
) (rawJWT string, err error) {
	return client.getBrowserJWT(ctx, serverURL, loginParams, onOpenBrowser)
}

func (client *AuthClient) getBrowserJWT(
	ctx context.Context,
	serverURL *url.URL,
	loginParams url.Values,
	onOpenBrowser func(string),
) (rawJWT string, err error) {
	li, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("failed to start listener: %w", err)
	}
	defer li.Close()
	callbackPath, err := newBrowserJWTCallbackPath()
	if err != nil {
		return "", fmt.Errorf("failed to create browser callback: %w", err)
	}

	incomingJWT := make(chan string, 1)
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return client.runHTTPServer(ctx, li, callbackPath, incomingJWT)
	})
	eg.Go(func() error {
		return client.runOpenBrowser(ctx, li, callbackPath, serverURL, loginParams, onOpenBrowser)
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

func newBrowserJWTCallbackPath() (string, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return "/callback/" + base64.RawURLEncoding.EncodeToString(nonce[:]), nil
}

func (client *AuthClient) runHTTPServer(
	ctx context.Context,
	li net.Listener,
	callbackPath string,
	incomingJWT chan<- string,
) error {
	srv := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}
	srv.Handler = &browserJWTCallbackHandler{
		ctx:          ctx,
		expectedHost: li.Addr().String(),
		expectedPath: callbackPath,
		incomingJWT:  incomingJWT,
		onAccepted: func() {
			go func() { _ = srv.Shutdown(ctx) }()
		},
	}
	// shutdown the server when ctx is done.
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	err := srv.Serve(li)
	if err == http.ErrServerClosed {
		err = nil
	}
	return err
}

type browserJWTCallbackHandler struct {
	ctx          context.Context
	expectedHost string
	expectedPath string
	incomingJWT  chan<- string
	onAccepted   func()
	accepted     atomic.Bool
}

func (h *browserJWTCallbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Host != h.expectedHost || r.URL.EscapedPath() != h.expectedPath {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	jwtValues, ok := query["pomerium_jwt"]
	if err != nil || len(query) != 1 || !ok || len(jwtValues) != 1 || jwtValues[0] == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !h.accepted.CompareAndSwap(false, true) {
		http.Error(w, "callback already consumed", http.StatusConflict)
		return
	}
	select {
	case h.incomingJWT <- jwtValues[0]:
	case <-h.ctx.Done():
		http.Error(w, "login canceled", http.StatusRequestTimeout)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, "login complete, you may close this page")
	if h.onAccepted != nil {
		h.onAccepted()
	}
}

func (client *AuthClient) runOpenBrowser(
	ctx context.Context,
	li net.Listener,
	callbackPath string,
	serverURL *url.URL,
	loginParams url.Values,
	onOpenBrowser func(string),
) error {
	browserURL := getBrowserURL(serverURL)
	query := make(url.Values, len(loginParams)+1)
	for key, values := range loginParams {
		query[key] = append([]string(nil), values...)
	}
	// The callback is owned by this client. Never permit caller-supplied
	// parameters to redirect the freshly issued credential elsewhere.
	callbackURL := &url.URL{
		Scheme: "http",
		Host:   li.Addr().String(),
		Path:   callbackPath,
	}
	query.Set("pomerium_redirect_uri", callbackURL.String())
	dst := browserURL.ResolveReference(&url.URL{
		Path:     "/.pomerium/api/v1/login",
		RawQuery: query.Encode(),
	})

	req, err := http.NewRequest("GET", dst.String(), nil)
	if err != nil {
		return err
	}

	bs, err := httputil.Fetch(ctx, client.cfg.tlsConfig, req)
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
