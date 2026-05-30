package httputil

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pomerium/cli/internal/testutil"
)

// TestFetchHTTPSTargetThroughProxy proves the auth path reaches an https target
// through a forward proxy with a single CONNECT hop, and that the target's TLS
// is still validated against the edge tlsConfig (over the tunnel) rather than
// skipped. It locks in the "no double-proxy, target TLS preserved" contract of
// the custom DialContext.
func TestFetchHTTPSTargetThroughProxy(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello through proxy"))
	}))
	defer target.Close()

	pool := x509.NewCertPool()
	pool.AddCert(target.Certificate())

	proxy := testutil.NewConnectProxy(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequest(http.MethodGet, target.URL, nil)
	require.NoError(t, err)

	body, err := Fetch(ctx, &tls.Config{RootCAs: pool}, req,
		WithProxyURL(&url.URL{Scheme: "http", Host: proxy.Addr}))
	require.NoError(t, err)
	assert.Equal(t, "hello through proxy", string(body))

	// Exactly one CONNECT, addressed to the target host:port (not the proxy).
	assert.Equal(t, int64(1), proxy.Conns(), "expected a single CONNECT hop")
	targetURL, err := url.Parse(target.URL)
	require.NoError(t, err)
	assert.Equal(t, targetURL.Host, proxy.Target())
}

// TestFetchProxyBasicAuth locks in that proxy credentials still reach the proxy
// on the auth path. With transport.Proxy nil, net/http no longer injects
// Proxy-Authorization, so the header must come from dialHTTPConnect using the
// proxy URL's userinfo.
func TestFetchProxyBasicAuth(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	pool := x509.NewCertPool()
	pool.AddCert(target.Certificate())

	proxy := testutil.NewConnectProxy(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequest(http.MethodGet, target.URL, nil)
	require.NoError(t, err)

	_, err = Fetch(ctx, &tls.Config{RootCAs: pool}, req,
		WithProxyURL(&url.URL{Scheme: "http", User: url.UserPassword("alice", "s3cret"), Host: proxy.Addr}))
	require.NoError(t, err)

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))
	assert.Equal(t, want, proxy.ProxyAuth(), "proxy credentials must reach the proxy via CONNECT")
}

// TestFetchHTTPSProxyValidatesAgainstSystemTrust guards the auth-path trust fix:
// an https forward proxy's certificate must be validated against system trust,
// not the edge tlsConfig pool. The proxy cert here is trusted by the edge pool
// (which would have satisfied the old transport.Proxy path) but not by system
// trust, so the dial must fail with the system-trust error.
func TestFetchHTTPSProxyValidatesAgainstSystemTrust(t *testing.T) {
	cert, leaf := selfSignedCert(t, "127.0.0.1")
	proxyAddr := startTLSProxy(t, cert)

	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequest(http.MethodGet, "https://edge.example.com/path", nil)
	require.NoError(t, err)

	_, err = Fetch(ctx, &tls.Config{RootCAs: pool}, req,
		WithProxyURL(&url.URL{Scheme: "https", Host: proxyAddr}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "system-trusted")
}

func selfSignedCert(t *testing.T, host string) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, leaf
}

// startTLSProxy starts a TLS listener that presents cert and drives the TLS
// handshake on each connection. A client that does not trust cert fails during
// the handshake, before any CONNECT is exchanged.
func startTLSProxy(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	li, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
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
				if tc, ok := conn.(*tls.Conn); ok {
					_ = tc.Handshake()
				}
			}()
		}
	}()
	return li.Addr().String()
}
