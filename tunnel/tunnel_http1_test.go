package tunnel

import (
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pomerium/cli/internal/testutil"
)

// A completed TunnelTCP must not leave a goroutine parked on ctx.Done while the
// parent context stays alive. Non-parallel so the goroutine count stays stable.
func TestHTTP1TunnelTCPNoGoroutineLeak(t *testing.T) {
	testutil.ClearProxyEnv(t)

	edge, _ := fakeEdge(t)
	ctx := t.Context()

	run := func() {
		tun := &http1tunneler{getConfig(
			WithDestinationHost("example.com:9999"),
			WithProxyHost(edge),
		)}
		c1, c2 := net.Pipe()
		// one line then EOF: the backend closes per line, unwinding the chain.
		go func() {
			_, _ = c1.Write([]byte("x\n"))
			_ = c1.Close()
		}()
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
	// closes; a leaked ctx.Done waiter never would. Poll until the count settles.
	var leaked int
	for range 200 {
		if leaked = runtime.NumGoroutine() - base; leaked < n/2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Less(t, leaked, n/2, "TunnelTCP leaked goroutines after connections closed")
}
