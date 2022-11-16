package authclient

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

		go func() {
			h := chi.NewMux()
			h.Get("/.pomerium/api/v1/login", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(r.FormValue("pomerium_redirect_uri")))
			})
			srv := &http.Server{
				BaseContext: func(li net.Listener) context.Context {
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
