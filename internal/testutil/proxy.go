package testutil

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// RecordingProxy is a fake forward proxy for tests. It records the connections
// it accepts, the CONNECT/SOCKS5 target it was asked to reach, and any
// Proxy-Authorization header, then splices through to that target.
type RecordingProxy struct {
	Addr     string
	upstream string // when set, splice here instead of the requested target
	conns    atomic.Int64
	target   atomic.Value
	auth     atomic.Value
}

// Conns returns how many connections the proxy accepted.
func (p *RecordingProxy) Conns() int64 { return p.conns.Load() }

// Target returns the last target the proxy was asked to reach.
func (p *RecordingProxy) Target() string { s, _ := p.target.Load().(string); return s }

// ProxyAuth returns the last Proxy-Authorization header value (CONNECT only).
func (p *RecordingProxy) ProxyAuth() string { s, _ := p.auth.Load().(string); return s }

func listen(t *testing.T) (net.Listener, *RecordingProxy) {
	t.Helper()
	li, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = li.Close() })
	return li, &RecordingProxy{Addr: li.Addr().String()}
}

// NewConnectProxy starts an HTTP CONNECT proxy that splices to the requested
// target.
func NewConnectProxy(t *testing.T) *RecordingProxy {
	return newConnectProxy(t, "")
}

// NewConnectProxyTo starts an HTTP CONNECT proxy that splices every CONNECT to
// upstream, ignoring the requested target (which is still recorded). Use this
// when the advertised edge host must be non-loopback so HTTP(S)_PROXY engages.
func NewConnectProxyTo(t *testing.T, upstream string) *RecordingProxy {
	return newConnectProxy(t, upstream)
}

func newConnectProxy(t *testing.T, upstream string) *RecordingProxy {
	li, p := listen(t)
	p.upstream = upstream
	go func() {
		for {
			conn, err := li.Accept()
			if err != nil {
				return
			}
			go p.handleConnect(conn)
		}
	}()
	return p
}

// ClearProxyEnv blanks every proxy environment variable for the test.
func ClearProxyEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy"} {
		t.Setenv(k, "")
	}
}

func (p *RecordingProxy) handleConnect(conn net.Conn) {
	defer conn.Close()
	p.conns.Add(1)

	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	var target string
	if f := strings.Fields(line); len(f) >= 2 {
		target = f[1]
		p.target.Store(target)
	}
	for {
		h, err := br.ReadString('\n')
		if err != nil {
			return
		}
		if strings.HasPrefix(strings.ToLower(h), "proxy-authorization:") {
			_, val, _ := strings.Cut(h, ":")
			p.auth.Store(strings.TrimSpace(val))
		}
		if h == "\r\n" || h == "\n" {
			break
		}
	}
	_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n")

	dst := target
	if p.upstream != "" {
		dst = p.upstream
	}
	out, err := net.Dial("tcp", dst)
	if err != nil {
		return
	}
	defer out.Close()
	splice(conn, br, out)
}

// NewSOCKS5Proxy starts a no-auth SOCKS5 proxy that splices to the requested
// target.
func NewSOCKS5Proxy(t *testing.T) *RecordingProxy {
	li, p := listen(t)
	go func() {
		for {
			conn, err := li.Accept()
			if err != nil {
				return
			}
			go p.handleSOCKS5(conn)
		}
	}()
	return p
}

func (p *RecordingProxy) handleSOCKS5(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)

	// greeting: VER, NMETHODS, METHODS...
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(br, greeting); err != nil {
		return
	}
	if _, err := io.ReadFull(br, make([]byte, int(greeting[1]))); err != nil {
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil { // no auth
		return
	}

	// request: VER CMD RSV ATYP DST.ADDR DST.PORT
	req := make([]byte, 4)
	if _, err := io.ReadFull(br, req); err != nil {
		return
	}
	var host string
	switch req[3] {
	case 0x01: // IPv4
		b := make([]byte, 4)
		if _, err := io.ReadFull(br, b); err != nil {
			return
		}
		host = net.IP(b).String()
	case 0x03: // domain
		l := make([]byte, 1)
		if _, err := io.ReadFull(br, l); err != nil {
			return
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(br, b); err != nil {
			return
		}
		host = string(b)
	default:
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(br, portBytes); err != nil {
		return
	}
	target := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(portBytes))))
	p.conns.Add(1)
	p.target.Store(target)

	// reply success with a dummy bound address (0.0.0.0:0).
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	out, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer out.Close()
	splice(conn, br, out)
}

func splice(client net.Conn, clientReader io.Reader, upstream net.Conn) {
	errc := make(chan error, 2)
	go func() { _, e := io.Copy(upstream, clientReader); errc <- e }()
	go func() { _, e := io.Copy(client, upstream); errc <- e }()
	<-errc
}
