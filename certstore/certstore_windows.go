//go:build windows && cgo

package certstore

import (
	"crypto/tls"
	"crypto/x509"

	"github.com/pomerium/cli/third_party/ecpsigner/windows/ncrypt"
	"github.com/pomerium/cli/version"
)

var IsCertstoreSupported = true

func init() {
	version.Features = append(version.Features, "ncrypt")
}

// loadCert searches the Windows trust store for a client certificate,
// according to a list of acceptable CA Distinguished Names and an additional
// filter.
func loadCert(
	acceptableCAs [][]byte, filterCallback func(*x509.Certificate) bool,
) (*tls.Certificate, error) {
	// Try the MY store in both the CURRENT_USER and LOCAL_MACHINE locations.
	cred, err := ncrypt.Cred(acceptableCAs, filterCallback, "MY", "current_user")
	if err == nil {
		return toTLSCertificate(cred), nil
	}
	cred, err = ncrypt.Cred(acceptableCAs, filterCallback, "MY", "local_machine")
	if err == nil {
		return toTLSCertificate(cred), nil
	}
	return nil, err
}
