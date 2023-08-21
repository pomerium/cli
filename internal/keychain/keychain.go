package keychain

import (
	"crypto/tls"
)

// LoadClientCertificates loads client certificates from the system key chain that
// match the given common name and/or organizational unit.
func LoadClientCertificates(commonName, organizationalUnit string) ([]tls.Certificate, error) {
	return loadClientCertificates(commonName, organizationalUnit)
}
