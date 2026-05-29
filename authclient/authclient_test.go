package authclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pomerium/cli/internal/testutil"
)

// TestCheckBearerTokenViaProxy verifies the auth path routes through a forward
// proxy. Auth uses an HTTPS server so http.Transport issues a CONNECT.
func TestCheckBearerTokenViaProxy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	proxy := testutil.NewConnectProxy(t)

	ac := New(WithForwardProxy(proxy.Addr))
	ac.cfg.tlsConfig = &tls.Config{RootCAs: pool}

	serverURL, err := url.Parse(srv.URL)
	require.NoError(t, err)

	require.NoError(t, ac.CheckBearerToken(ctx, serverURL, "token"))
	assert.Positive(t, proxy.Conns(), "auth request should route through the forward proxy")
	assert.Equal(t, serverURL.Host, proxy.Target())
}

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

		go func() {
			h := chi.NewMux()
			h.Get("/.pomerium/api/v1/login", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(r.FormValue("pomerium_redirect_uri")))
			})
			srv := &http.Server{
				BaseContext: func(_ net.Listener) context.Context {
					return ctx
				},
				Handler: h,
			}
			_ = srv.Serve(li)
		}()

		ac := New()
		ac.cfg.open = func(input string) error {
			u, err := url.Parse(input)
			if err != nil {
				return err
			}
			u = u.ResolveReference(&url.URL{
				RawQuery: url.Values{
					"pomerium_jwt": {"TEST"},
				}.Encode(),
			})

			req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
			if err != nil {
				return err
			}

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			_ = res.Body.Close()
			return nil
		}

		rawJWT, err := ac.GetJWT(ctx, &url.URL{
			Scheme: "http",
			Host:   li.Addr().String(),
		}, func(_ string) {})
		assert.NoError(t, err)
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
