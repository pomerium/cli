//go:build darwin && cgo

package certstore

import (
	"crypto/tls"
	"crypto/x509"

	"github.com/pomerium/cli/third_party/ecpsigner/darwin/keychain"
	"github.com/pomerium/cli/version"
)

var IsCertstoreSupported = true

func init() {
	version.Features = append(version.Features, "keychain")
}

// loadCert searches the macOS Keychain for a client certificate, according to
// a list of acceptable CA Distinguished Names and an additional filter.
func loadCert(
	acceptableCAs [][]byte, filterCallback func(*x509.Certificate) bool,
) (*tls.Certificate, error) {
	cred, err := keychain.Cred(acceptableCAs, filterCallback)
	if err != nil {
		return nil, err
	}
	return toTLSCertificate(cred), nil
}
