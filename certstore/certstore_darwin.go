//go:build darwin && cgo

package certstore

import (
	"crypto/tls"

	"github.com/pomerium/cli/third_party/ecpsigner/darwin/keychain"
	"github.com/pomerium/cli/version"
)

var IsCertstoreSupported = true

func init() {
	version.Features = append(version.Features, "keychain")
}

func LoadCert(issuer string) (*tls.Certificate, error) {
	cred, err := keychain.Cred(issuer)
	if err != nil {
		return nil, err
	}
	return toTLSCertificate(cred), nil
}
