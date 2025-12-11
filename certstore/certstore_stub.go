//go:build !(cgo && (darwin || windows))

package certstore

import (
	"crypto/tls"
	"crypto/x509"
)

// IsCertstoreSupported indicates that the cert store is not supported.
var IsCertstoreSupported = false

// loadCert is a stub that always returns an error, for builds where this
// feature is not supported.
func loadCert([][]byte, func(*x509.Certificate) bool) (*tls.Certificate, error) {
	return nil, errNotSupported
}
