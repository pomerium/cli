package httputil

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pomerium/cli/internal/testutil"
)

func TestResolveProxy(t *testing.T) {
	httpsEdge := &url.URL{Scheme: "https", Host: "edge.example.com:443"}
	httpEdge := &url.URL{Scheme: "http", Host: "edge.example.com:80"}

	for _, tc := range []struct {
		name     string
		override string
		env      map[string]string
		edge     *url.URL
		want     string
		wantErr  bool
	}{
		{name: "override url", override: "http://proxy:8080", edge: httpsEdge, want: "http://proxy:8080"},
		{name: "override bare host:port", override: "proxy:8080", edge: httpsEdge, want: "http://proxy:8080"},
		{name: "override socks5", override: "socks5://proxy:1080", edge: httpsEdge, want: "socks5://proxy:1080"},
		{name: "override socks5h", override: "socks5h://proxy:1080", edge: httpsEdge, want: "socks5h://proxy:1080"},
		{name: "override trims whitespace", override: "  http://proxy:8080  ", edge: httpsEdge, want: "http://proxy:8080"},
		{name: "override trailing slash ok", override: "http://proxy:8080/", edge: httpsEdge, want: "http://proxy:8080"},
		{name: "override socks5 trailing slash ok", override: "socks5://proxy:1080/", edge: httpsEdge, want: "socks5://proxy:1080"},
		{name: "override bare host trailing slash ok", override: "proxy:8080/", edge: httpsEdge, want: "http://proxy:8080"},
		{name: "https_proxy", env: map[string]string{"HTTPS_PROXY": "http://hp:3128"}, edge: httpsEdge, want: "http://hp:3128"},
		{name: "http_proxy", env: map[string]string{"HTTP_PROXY": "http://hp:3128"}, edge: httpEdge, want: "http://hp:3128"},
		{name: "no_proxy", env: map[string]string{"HTTPS_PROXY": "http://hp:3128", "NO_PROXY": "edge.example.com"}, edge: httpsEdge, want: ""},
		{name: "all_proxy socks5", env: map[string]string{"ALL_PROXY": "socks5://sp:1080"}, edge: httpsEdge, want: "socks5://sp:1080"},
		{name: "all_proxy honors no_proxy", env: map[string]string{"ALL_PROXY": "socks5://sp:1080", "NO_PROXY": "edge.example.com"}, edge: httpsEdge, want: ""},
		{name: "override beats env", override: "http://override:9", env: map[string]string{"HTTPS_PROXY": "http://hp:3128"}, edge: httpsEdge, want: "http://override:9"},
		{name: "whitespace override is unset", override: "   ", env: map[string]string{"HTTPS_PROXY": "http://hp:3128"}, edge: httpsEdge, want: "http://hp:3128"},
		{name: "none", edge: httpsEdge, want: ""},
		{name: "bad scheme", override: "socks4://p:1", edge: httpsEdge, wantErr: true},
		{name: "path rejected", override: "http://p:8080/path", edge: httpsEdge, wantErr: true},
		{name: "query rejected", override: "http://p:8080?q=1", edge: httpsEdge, wantErr: true},
		{name: "no host", override: "http://", edge: httpsEdge, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.ClearProxyEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			got, err := ResolveProxy(tc.override, tc.edge)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.want == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.String())
		})
	}
}

func TestResolveProxyRedactsCredentials(t *testing.T) {
	edge := &url.URL{Scheme: "https", Host: "edge:443"}
	for _, override := range []string{
		"http://user:hunter2@proxy:8080/path", // rejected after parsing
		"http://user:hunter2@proxy:badport",   // url.Parse failure
		"http://user:hunter2%@proxy:8080",     // invalid escape in the password itself
	} {
		_, err := ResolveProxy(override, edge)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "hunter2", "override %q", override)
	}

	// url.EscapeError quotes the bytes around a bad escape ("%te" here), which
	// would leak a password fragment.
	_, err := ResolveProxy("http://user:hun%ter2@proxy:8080", edge)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "%te")
}

func TestValidateForwardProxyFlag(t *testing.T) {
	// The explicit-flag validator must never consult the environment, otherwise
	// env-proxy errors would surface at startup instead of at request time.
	t.Setenv("HTTPS_PROXY", "http://env-proxy:3128")
	t.Setenv("ALL_PROXY", "socks5://env-proxy:1080")

	u, err := ValidateForwardProxyFlag("")
	require.NoError(t, err)
	assert.Nil(t, u, "empty flag is valid and must not resolve env proxies")

	u, err = ValidateForwardProxyFlag("proxy:8080")
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, "http://proxy:8080", u.String())
}

// Plain-http targets keep the default transport's absolute-form env proxying;
// an explicit override always routes through the custom dialer.
func TestProxyFetchOptions(t *testing.T) {
	testutil.ClearProxyEnv(t)
	t.Setenv("HTTP_PROXY", "http://hp:3128")
	t.Setenv("HTTPS_PROXY", "http://hp:3128")

	httpEdge := &url.URL{Scheme: "http", Host: "edge.example.com:80"}
	httpsEdge := &url.URL{Scheme: "https", Host: "edge.example.com:443"}

	opts, err := ProxyFetchOptions("", httpEdge)
	require.NoError(t, err)
	assert.Empty(t, opts, "env proxy + plain-http target stays on the default transport")

	// the carve-out must not pre-empt env errors the default transport owns.
	t.Setenv("ALL_PROXY", "::bad::")
	opts, err = ProxyFetchOptions("", httpEdge)
	require.NoError(t, err)
	assert.Empty(t, opts)
	t.Setenv("ALL_PROXY", "")

	opts, err = ProxyFetchOptions("", httpsEdge)
	require.NoError(t, err)
	assert.Len(t, opts, 1)

	opts, err = ProxyFetchOptions("op:9", httpEdge)
	require.NoError(t, err)
	assert.Len(t, opts, 1, "explicit override applies to plain-http targets")
}

func TestDialThroughProxyHTTPConnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	backend := echoListener(t)
	proxy := testutil.NewConnectProxy(t)

	conn, err := DialThroughProxy(ctx, &url.URL{Scheme: "http", Host: proxy.Addr}, backend)
	require.NoError(t, err)
	defer conn.Close()

	assert.Equal(t, "ping\n", roundTrip(t, conn))
	assert.Equal(t, backend, proxy.Target())
}

func TestDialThroughProxySOCKS5(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	backend := echoListener(t)
	proxy := testutil.NewSOCKS5Proxy(t)

	conn, err := DialThroughProxy(ctx, &url.URL{Scheme: "socks5", Host: proxy.Addr}, backend)
	require.NoError(t, err)
	defer conn.Close()

	assert.Equal(t, "ping\n", roundTrip(t, conn))
	assert.Equal(t, backend, proxy.Target())
}

func TestDialThroughProxyConnectRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	proxyLi, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer proxyLi.Close()
	go func() {
		conn, err := proxyLi.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		for {
			h, err := br.ReadString('\n')
			if err != nil || h == "\r\n" || h == "\n" {
				break
			}
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\n\r\n")
	}()

	_, err = DialThroughProxy(ctx, &url.URL{Scheme: "http", Host: proxyLi.Addr().String()}, "edge.example.com:443")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

// TestDialThroughProxyHTTPSProxyError verifies that a TLS failure to an https
// proxy reports the system-trust limitation without leaking credentials. The
// listener speaks plaintext, so the handshake fails.
func TestDialThroughProxyHTTPSProxyError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	li, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer li.Close()
	go func() {
		if c, err := li.Accept(); err == nil {
			_ = c.Close()
		}
	}()

	_, err = DialThroughProxy(ctx, &url.URL{Scheme: "https", User: url.UserPassword("u", "hunter2"), Host: li.Addr().String()}, "edge:443")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "system-trusted")
	assert.NotContains(t, err.Error(), "hunter2")
}

// TestDialThroughProxyContextCancel verifies a stalled CONNECT exchange unblocks
// promptly when the context is canceled (via the AfterFunc guard).
func TestDialThroughProxyContextCancel(t *testing.T) {
	li, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer li.Close()
	go func() {
		conn, err := li.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn) // read forever, never reply
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()

	done := make(chan error, 1)
	go func() {
		_, err := DialThroughProxy(ctx, &url.URL{Scheme: "http", Host: li.Addr().String()}, "edge:443")
		done <- err
	}()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("DialThroughProxy did not return after context cancellation")
	}
}

func echoListener(t *testing.T) string {
	t.Helper()
	li, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = li.Close() })
	go func() {
		conn, err := li.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()
	return li.Addr().String()
}

func roundTrip(t *testing.T, conn net.Conn) string {
	t.Helper()
	_, err := io.WriteString(conn, "ping\n")
	require.NoError(t, err)
	got, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, err)
	return got
}
