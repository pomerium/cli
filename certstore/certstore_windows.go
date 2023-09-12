//go:build windows && cgo

package certstore

import (
	"crypto/tls"

	"github.com/pomerium/cli/third_party/ecpsigner/windows/ncrypt"
	"github.com/pomerium/cli/version"
)

var IsCertstoreSupported = true

func init() {
	version.Features = append(version.Features, "ncrypt")
}

func LoadCert(issuer string) (*tls.Certificate, error) {
	// Try the MY store in both the CURRENT_USER and LOCAL_MACHINE locations.
	cred, err := ncrypt.Cred(issuer, "MY", "current_user")
	if err == nil {
		return toTLSCertificate(cred), nil
	}
	cred, err = ncrypt.Cred(issuer, "MY", "local_machine")
	if err == nil {
		return toTLSCertificate(cred), nil
	}
	return nil, err
}
