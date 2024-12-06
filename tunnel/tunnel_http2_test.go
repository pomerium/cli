package tunnel

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTCPTunnelViaHTTP2(t *testing.T) {
	t.Parallel()

	ctx, clearTimeout := context.WithTimeout(context.Background(), time.Second*10)
	defer clearTimeout()

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, "CONNECT", r.Method) {
			return
		}
		if !assert.Equal(t, "Pomerium JWT", r.Header.Get("Authorization")) {
			return
		}
		if !assert.Equal(t, "example.com:9999", r.Host) {
			return
		}

		defer r.Body.Close()
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()

		buf := make([]byte, 4)
		_, err := io.ReadFull(r.Body, buf)
		assert.NoError(t, err)
		assert.Equal(t, []byte{1, 2, 3, 4}, buf)

		_, _ = w.Write([]byte{5, 6, 7, 8})
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()

	c1, c2 := net.Pipe()
	go func() {
		_, _ = c1.Write([]byte{1, 2, 3, 4})
	}()
	go func() {
		buf := make([]byte, 4)
		_, err := io.ReadFull(c1, buf)
		assert.NoError(t, err)
		assert.Equal(t, []byte{5, 6, 7, 8}, buf)
		_ = c1.Close()
	}()

	tun := &http2tunneler{
		getConfig(
			WithDestinationHost("example.com:9999"),
			WithProxyHost(srv.Listener.Addr().String()),
			WithTLSConfig(&tls.Config{
				InsecureSkipVerify: true,
			}),
		),
	}
	err := tun.TunnelTCP(ctx, DiscardEvents(), c2, "JWT")
	assert.NoError(t, err)
}
