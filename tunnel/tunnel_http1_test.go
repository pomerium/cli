package tunnel

import (
	"bufio"
	"io"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTP1TunnelTCPNoGoroutineLeak guards the per-connection goroutine leak
// fix: a completed TunnelTCP must not leave a goroutine parked on ctx.Done while
// the parent context stays alive. It is intentionally non-parallel so it runs in
// isolation from the package's t.Parallel tests, keeping the goroutine count
// stable.
func TestHTTP1TunnelTCPNoGoroutineLeak(t *testing.T) {
	clearProxyEnv(t)

	edge := startCONNECTEdge(t)
	ctx := t.Context()

	run := func() {
		tun := &http1tunneler{getConfig(
			WithDestinationHost("example.com:9999"),
			WithProxyHost(edge),
		)}
		c1, c2 := net.Pipe()
		_ = c1.Close() // local EOF: the tunnel completes immediately
		require.NoError(t, tun.TunnelTCP(ctx, DiscardEvents(), c2, ""))
		_ = c2.Close()
	}

	run() // warm up one-time goroutines before sampling the baseline
	base := runtime.NumGoroutine()

	const n = 30
	for range n {
		run()
	}

	// Transient goroutines (edge handlers, copy loops) exit once each connection
	// closes; the buggy ctx.Done waiter never would. Poll until the count settles.
	var leaked int
	for range 200 {
		if leaked = runtime.NumGoroutine() - base; leaked < n/2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Less(t, leaked, n/2, "TunnelTCP leaked goroutines after connections closed")
}

// startCONNECTEdge accepts CONNECT requests, replies 200, then drains until the
// client closes. It serves the goroutine-leak test as a minimal Pomerium edge.
func startCONNECTEdge(t *testing.T) string {
	t.Helper()
	li, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = li.Close() })
	go func() {
		for {
			conn, err := li.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				br := bufio.NewReader(conn)
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if line == "\r\n" {
						break
					}
				}
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\n\r\n")
				_, _ = io.Copy(io.Discard, conn)
			}()
		}
	}()
	return li.Addr().String()
}
