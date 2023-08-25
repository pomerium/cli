//go:build cgo && (darwin || windows)

package certstore

import (
	"crypto"
	"crypto/tls"
)

type credential interface {
	crypto.Signer
	CertificateChain() [][]byte
}

func toTLSCertificate(cred credential) *tls.Certificate {
	return &tls.Certificate{
		Certificate: cred.CertificateChain(),
		PrivateKey:  cred,
	}
}
