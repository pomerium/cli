package tunnel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pomerium/cli/internal/testutil"
)

// fakeEdge starts a backend plus a Pomerium-edge server that terminates the
// tunnel CONNECT and splices to the backend. It returns the edge address and a
// channel that receives the first line the backend reads from the tunnel.
func fakeEdge(t *testing.T) (edgeAddr string, got <-chan string) {
	t.Helper()
	gotc := make(chan string, 1)

	backend, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = backend.Close() })
	go func() {
		for {
			conn, err := backend.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				ln, _, _ := bufio.NewReader(conn).ReadLine()
				select {
				case gotc <- string(ln):
				default:
				}
			}()
		}
	}()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, "CONNECT", r.Method) {
			return
		}
		w.WriteHeader(200)
		in, brw, err := w.(http.Hijacker).Hijack()
		if !assert.NoError(t, err) {
			return
		}
		defer in.Close()
		out, err := net.Dial("tcp", backend.Addr().String())
		if !assert.NoError(t, err) {
			return
		}
		defer out.Close()
		errc := make(chan error, 2)
		go func() { _, e := io.Copy(in, out); errc <- e }()
		go func() { _, e := io.Copy(out, deBuffer(brw.Reader, in)); errc <- e }()
		<-errc
	}))
	t.Cleanup(srv.Close)

	return srv.Listener.Addr().String(), gotc
}

func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy"} {
		t.Setenv(k, "")
	}
}

// runTCPTunnel drives one "HELLO WORLD" line through the tunnel and returns what
// the backend received.
func runTCPTunnel(t *testing.T, got <-chan string, opts ...Option) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := New(opts...).Run(ctx, readWriter{strings.NewReader("HELLO WORLD\n"), &buf}, DiscardEvents())
	require.NoError(t, err)

	select {
	case line := <-got:
		assert.Equal(t, "HELLO WORLD", line)
	case <-time.After(5 * time.Second):
		t.Fatal("backend never received the tunneled data")
	}
}

func TestTunnelViaForwardProxyFlag(t *testing.T) {
	clearProxyEnv(t)
	edge, got := fakeEdge(t)
	proxy := testutil.NewConnectProxy(t)

	runTCPTunnel(t, got,
		WithDestinationHost("example.com:9999"),
		WithProxyHost(edge),
		WithForwardProxy(proxy.Addr))

	assert.Equal(t, edge, proxy.Target())
}

// Env-var proxies never apply to loopback edges (Go's httpproxy bypasses
// localhost), so these tests advertise a non-loopback edge name and use a proxy
// that splices to the real loopback edge. NO_PROXY and override precedence at
// the resolution layer are covered by TestResolveProxy.
func TestTunnelHonorsEnvProxy(t *testing.T) {
	clearProxyEnv(t)
	edge, got := fakeEdge(t)
	proxy := testutil.NewConnectProxyTo(t, edge)
	t.Setenv("HTTP_PROXY", "http://"+proxy.Addr)

	runTCPTunnel(t, got,
		WithDestinationHost("example.com:9999"),
		WithProxyHost("edge.example.com:443"))

	assert.Equal(t, "edge.example.com:443", proxy.Target(), "tunnel should route through HTTP_PROXY")
}

func TestTunnelFlagOverridesEnv(t *testing.T) {
	clearProxyEnv(t)
	edge, got := fakeEdge(t)
	envProxy := testutil.NewConnectProxyTo(t, edge)
	flagProxy := testutil.NewConnectProxyTo(t, edge)
	t.Setenv("HTTP_PROXY", "http://"+envProxy.Addr)

	runTCPTunnel(t, got,
		WithDestinationHost("example.com:9999"),
		WithProxyHost("edge.example.com:443"),
		WithForwardProxy(flagProxy.Addr))

	assert.Positive(t, flagProxy.Conns(), "explicit flag proxy should be used")
	assert.Zero(t, envProxy.Conns(), "env proxy should be ignored when the flag is set")
}

func TestTunnelViaSOCKS5(t *testing.T) {
	clearProxyEnv(t)
	edge, got := fakeEdge(t)
	proxy := testutil.NewSOCKS5Proxy(t)

	runTCPTunnel(t, got,
		WithDestinationHost("example.com:9999"),
		WithProxyHost(edge),
		WithForwardProxy("socks5://"+proxy.Addr))

	assert.Equal(t, edge, proxy.Target())
}

func TestTunnelUnsupportedProxyScheme(t *testing.T) {
	clearProxyEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := New(
		WithDestinationHost("example.com:9999"),
		WithProxyHost("edge.example.com:443"),
		WithForwardProxy("ftp://proxy:21"),
	).Run(ctx, readWriter{strings.NewReader("HELLO WORLD\n"), &buf}, DiscardEvents())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported forward proxy scheme")
}

func TestTunnelProxyCredentialsNotLogged(t *testing.T) {
	clearProxyEnv(t)
	edge, got := fakeEdge(t)
	proxy := testutil.NewConnectProxy(t)

	var logBuf bytes.Buffer
	ctx := zerolog.New(&logBuf).WithContext(context.Background())
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := New(
		WithDestinationHost("example.com:9999"),
		WithProxyHost(edge),
		WithForwardProxy("http://user:s3cret@"+proxy.Addr),
	).Run(ctx, readWriter{strings.NewReader("HELLO WORLD\n"), &buf}, DiscardEvents())
	require.NoError(t, err)

	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("backend never received the tunneled data")
	}

	assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("user:s3cret")), proxy.ProxyAuth())
	assert.NotContains(t, logBuf.String(), "s3cret", "credentials must never be logged")
}
