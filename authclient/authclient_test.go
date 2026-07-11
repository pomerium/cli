package authclient

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthClient(t *testing.T) {
	t.Parallel()

	ctx, clearTimeout := context.WithTimeout(context.Background(), time.Second*30)
	t.Cleanup(clearTimeout)

	t.Run("browser", func(t *testing.T) {
		t.Parallel()

		li, err := net.Listen("tcp", "127.0.0.1:0")
		if !assert.NoError(t, err) {
			return
		}
		t.Cleanup(func() { li.Close() })
		expectedLoginParams := make(chan url.Values, 2)
		loginRequestResults := make(chan error, 2)

		go func() {
			h := chi.NewMux()
			h.Get("/.pomerium/api/v1/login", func(w http.ResponseWriter, r *http.Request) {
				callback, err := validateBrowserLoginRequest(r.URL.Query(), <-expectedLoginParams)
				loginRequestResults <- err
				if err != nil {
					http.Error(w, "invalid login request", http.StatusBadRequest)
					return
				}
				_, _ = w.Write([]byte(callback.String()))
			})
			srv := &http.Server{
				BaseContext: func(_ net.Listener) context.Context {
					return ctx
				},
				Handler: h,
			}
			_ = srv.Serve(li)
		}()

		ac := New(WithServiceAccount("MUST_NOT_BE_USED"))
		openBrowser := func(input string) error {
			u, err := url.Parse(input)
			if err != nil {
				return err
			}
			const injectedJWT = "CROSS-IDENTITY-JWT-CANARY"
			doCallback := func(method string, callback *url.URL, host, jwt string) (int, string, error) {
				candidate := *callback
				query := candidate.Query()
				query.Set("pomerium_jwt", jwt)
				candidate.RawQuery = query.Encode()
				req, err := http.NewRequestWithContext(ctx, method, candidate.String(), nil)
				if err != nil {
					return 0, "", err
				}
				if host != "" {
					req.Host = host
				}
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					return 0, "", err
				}
				defer res.Body.Close()
				body, err := io.ReadAll(res.Body)
				return res.StatusCode, string(body), err
			}

			wrongPath := *u
			wrongPath.Path = "/callback/not-the-issued-state"
			status, body, err := doCallback(http.MethodGet, &wrongPath, "", injectedJWT)
			if err != nil {
				return err
			}
			if status != http.StatusNotFound || strings.Contains(body, injectedJWT) {
				return fmt.Errorf("wrong-path callback: status=%d body=%q", status, body)
			}

			status, body, err = doCallback(http.MethodPost, u, "", injectedJWT)
			if err != nil {
				return err
			}
			if status != http.StatusMethodNotAllowed || strings.Contains(body, injectedJWT) {
				return fmt.Errorf("wrong-method callback: status=%d body=%q", status, body)
			}

			status, body, err = doCallback(http.MethodGet, u, "localhost:"+u.Port(), injectedJWT)
			if err != nil {
				return err
			}
			if status != http.StatusNotFound || strings.Contains(body, injectedJWT) {
				return fmt.Errorf("wrong-host callback: status=%d body=%q", status, body)
			}

			extraQuery := *u
			extraQuery.RawQuery = url.Values{"unexpected": {"EXTRA-QUERY-CANARY"}}.Encode()
			status, body, err = doCallback(http.MethodGet, &extraQuery, "", injectedJWT)
			if err != nil {
				return err
			}
			if status != http.StatusNotFound ||
				strings.Contains(body, injectedJWT) ||
				strings.Contains(body, "EXTRA-QUERY-CANARY") {
				return fmt.Errorf("extra-query callback: status=%d body=%q", status, body)
			}

			status, body, err = doCallback(http.MethodGet, u, "", "TEST")
			if err != nil {
				return err
			}
			if status != http.StatusOK || strings.Contains(body, "TEST") {
				return fmt.Errorf("valid callback: status=%d body=%q", status, body)
			}
			return nil
		}
		ac.cfg.open = openBrowser

		serverURL := &url.URL{
			Scheme: "http",
			Host:   li.Addr().String(),
		}
		loginParams := url.Values{
			"pomerium_route":        {"route.example.com"},
			"pomerium_redirect_uri": {"https://attacker.example/capture"},
		}
		originalLoginParams := cloneURLValues(loginParams)
		expectedLoginParams <- url.Values{"pomerium_route": {"route.example.com"}}
		rawJWT, err := ac.GetBrowserJWT(ctx, serverURL, loginParams, func(_ string) {})
		require.NoError(t, err)
		require.NoError(t, <-loginRequestResults)
		assert.Equal(t, "TEST", rawJWT)
		assert.Equal(t, originalLoginParams, loginParams, "caller-owned login parameters were mutated")

		ac = New()
		ac.cfg.open = openBrowser
		expectedLoginParams <- url.Values{}
		rawJWT, err = ac.GetJWT(ctx, serverURL, func(_ string) {})
		require.NoError(t, err)
		require.NoError(t, <-loginRequestResults)
		assert.Equal(t, "TEST", rawJWT)
	})

	t.Run("service account", func(t *testing.T) {
		t.Parallel()

		ac := New(WithServiceAccount("SERVICE_ACCOUNT"))
		rawJWT, err := ac.GetJWT(ctx, &url.URL{
			Scheme: "http",
			Host:   "example.com",
		}, func(_ string) {})
		assert.NoError(t, err)
		assert.Equal(t, "SERVICE_ACCOUNT", rawJWT)
	})

	t.Run("service account file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		err := os.WriteFile(filepath.Join(dir, "service-account"), []byte(`
			SERVICE_ACCOUNT
		`), 0o600)
		require.NoError(t, err)
		ac := New(WithServiceAccountFile(filepath.Join(dir, "service-account")))
		rawJWT, err := ac.GetJWT(ctx, &url.URL{
			Scheme: "http",
			Host:   "example.com",
		}, func(_ string) {})
		assert.NoError(t, err)
		assert.Equal(t, "SERVICE_ACCOUNT", rawJWT)
	})
}

func validateBrowserLoginRequest(query, expectedParams url.Values) (*url.URL, error) {
	redirectValues := query["pomerium_redirect_uri"]
	if len(redirectValues) != 1 {
		return nil, fmt.Errorf("expected exactly one redirect URI, got %d", len(redirectValues))
	}
	callback, err := url.Parse(redirectValues[0])
	if err != nil {
		return nil, fmt.Errorf("parse redirect URI: %w", err)
	}
	if callback.Scheme != "http" || callback.Hostname() != "127.0.0.1" || callback.Port() == "" {
		return nil, fmt.Errorf("redirect URI is not a loopback HTTP listener: %q", callback)
	}
	const callbackPrefix = "/callback/"
	if !strings.HasPrefix(callback.EscapedPath(), callbackPrefix) {
		return nil, fmt.Errorf("redirect URI has unexpected callback path: %q", callback.EscapedPath())
	}
	if len(strings.TrimPrefix(callback.EscapedPath(), callbackPrefix)) < 43 {
		return nil, fmt.Errorf("redirect URI callback state has insufficient entropy")
	}
	if callback.RawQuery != "" || callback.Fragment != "" || callback.User != nil {
		return nil, fmt.Errorf("redirect URI contains unexpected components: %q", callback)
	}

	actualParams := cloneURLValues(query)
	actualParams.Del("pomerium_redirect_uri")
	if actualParams.Encode() != expectedParams.Encode() {
		return nil, fmt.Errorf("login parameters mismatch: got %q, want %q", actualParams.Encode(), expectedParams.Encode())
	}
	return callback, nil
}

func cloneURLValues(values url.Values) url.Values {
	clone := make(url.Values, len(values))
	for key, entries := range values {
		clone[key] = append([]string(nil), entries...)
	}
	return clone
}

func TestBrowserJWTCallbackHandlerIsSingleUseAndNonblocking(t *testing.T) {
	t.Parallel()

	const (
		host = "127.0.0.1:49152"
		path = "/callback/issued-unpredictable-state"
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	incomingJWT := make(chan string, 1)
	handler := &browserJWTCallbackHandler{
		ctx:          ctx,
		expectedHost: host,
		expectedPath: path,
		incomingJWT:  incomingJWT,
	}

	type result struct {
		status int
		body   string
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for _, rawJWT := range []string{"FIRST-JWT-CANARY", "SECOND-JWT-CANARY"} {
		rawJWT := rawJWT
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			requestURL := "http://" + host + path + "?" + url.Values{"pomerium_jwt": {rawJWT}}.Encode()
			req := httptest.NewRequest(http.MethodGet, requestURL, nil)
			req.Host = host
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			results <- result{response.Code, response.Body.String()}
		}()
	}
	close(start)
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent callbacks did not complete")
	}

	statusCounts := map[int]int{}
	for range 2 {
		result := <-results
		statusCounts[result.status]++
		assert.NotContains(t, result.body, "FIRST-JWT-CANARY")
		assert.NotContains(t, result.body, "SECOND-JWT-CANARY")
	}
	assert.Equal(t, 1, statusCounts[http.StatusOK])
	assert.Equal(t, 1, statusCounts[http.StatusConflict])

	select {
	case got := <-incomingJWT:
		assert.Contains(t, []string{"FIRST-JWT-CANARY", "SECOND-JWT-CANARY"}, got)
	default:
		t.Fatal("winning callback did not deliver a JWT")
	}
	select {
	case duplicate := <-incomingJWT:
		t.Fatalf("more than one callback won: %q", duplicate)
	default:
	}
}

func TestBrowserJWTCallbackHandlerRejectsDuplicateJWTValues(t *testing.T) {
	t.Parallel()

	const (
		host = "127.0.0.1:49152"
		path = "/callback/issued-unpredictable-state"
	)
	incomingJWT := make(chan string, 1)
	handler := &browserJWTCallbackHandler{
		ctx:          t.Context(),
		expectedHost: host,
		expectedPath: path,
		incomingJWT:  incomingJWT,
	}

	requestURL := "http://" + host + path + "?pomerium_jwt=FIRST-JWT-CANARY&pomerium_jwt=SECOND-JWT-CANARY"
	req := httptest.NewRequest(http.MethodGet, requestURL, nil)
	req.Host = host
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	assert.Equal(t, http.StatusNotFound, response.Code)
	assert.NotContains(t, response.Body.String(), "FIRST-JWT-CANARY")
	assert.NotContains(t, response.Body.String(), "SECOND-JWT-CANARY")
	select {
	case jwt := <-incomingJWT:
		t.Fatalf("duplicate query values delivered a JWT: %q", jwt)
	default:
	}
}
