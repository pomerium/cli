package tunnel

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pomerium/cli/internal/testutil"
)

func TestUDPSessionManager(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ctx, clearTimeout := context.WithTimeout(ctx, 10*time.Second)
	defer clearTimeout()

	tunnelPort := testutil.GetPort(t)
	localPort := testutil.GetPort(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, r.Host, "example.com:9999")
		require.Equal(t, r.URL.Path, "/.well-known/masque/udp/example.com/9999/")
		w.Header().Set("Transfer-Encoding", "identity")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()

		in, brw, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		defer in.Close()

		payload, err := readUDPCapsuleDatagram(quicvarint.NewReader(in))
		require.NoError(t, err)
		require.Equal(t, []byte("\x00SEND HELLO WORLD"), payload)

		err = http3.WriteCapsule(quicvarint.NewWriter(brw), 0, []byte("\x00RECV HELLO WORLD"))
		require.NoError(t, err)
		err = brw.Flush()
		require.NoError(t, err)

		<-ctx.Done()
	}))
	defer srv.Close()

	tun := New(
		WithDestinationHost("example.com:9999"),
		WithProxyHost(srv.Listener.Addr().String()))

	// start the tunnel udp session manager

	tunnelAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:"+tunnelPort)
	require.NoError(t, err)

	tunnelConn, err := net.ListenUDP("udp", tunnelAddr)
	require.NoError(t, err)

	tunErrC := make(chan error, 1)
	go func() { tunErrC <- tun.RunUDPSessionManager(ctx, tunnelConn, LogEvents()) }()

	localAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:"+localPort)
	require.NoError(t, err)

	// create the local connection

	conn, err := net.ListenUDP("udp", localAddr)
	require.NoError(t, err)
	context.AfterFunc(ctx, func() { _ = conn.Close() })()

	n, err := conn.WriteToUDP([]byte("SEND HELLO WORLD"), tunnelAddr)
	assert.Equal(t, 16, n)
	assert.NoError(t, err)

	payload := make([]byte, maxUDPPacketSize)
	n, _, err = conn.ReadFromUDP(payload)
	assert.Equal(t, []byte("RECV HELLO WORLD"), payload[:n])
	assert.Equal(t, 16, n)
	assert.NoError(t, err)

	// cancel the context to stop the tunnel
	cancel()
	err = <-tunErrC
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	assert.NoError(t, err, "tunnel should shutdown cleanly")
}
