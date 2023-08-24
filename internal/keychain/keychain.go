package keychain

import (
	"crypto/tls"
)

// LoadClientCertificates loads client certificates from the system key chain.
// The returned function should be called to release any resources associated
// with the client certificates.
func LoadClientCertificates() ([]tls.Certificate, func(), error) {
	return loadClientCertificates()
}
