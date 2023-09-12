//go:build !(cgo && (darwin || windows))

package certstore

import (
	"crypto/tls"
	"errors"
)

var IsCertstoreSupported = false

func LoadCert(issuer string) (*tls.Certificate, error) {
	return nil, errors.New("this build of pomerium-cli does not support this feature")
}
