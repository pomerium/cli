// Copyright 2022 Google LLC.
// Copyright 2023 Pomerium Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build windows
// +build windows

// Cert_util provides helpers for working with Windows certificates via crypt32.dll

package ncrypt

import (
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// wincrypt.h constants
	signatureKeyUsage = 0x80 // CERT_DIGITAL_SIGNATURE_KEY_USAGE
)

var (
	null = uintptr(unsafe.Pointer(nil))

	crypt32 = windows.MustLoadDLL("crypt32.dll")

	certGetIntendedKeyUsage           = crypt32.MustFindProc("CertGetIntendedKeyUsage")
	cryptAcquireCertificatePrivateKey = crypt32.MustFindProc("CryptAcquireCertificatePrivateKey")
)

// extractSimpleChain extracts the first certificate chain from a CertSimpleChain.
// Adapted from crypto.x509.root_windows
func extractSimpleChain(
	simpleChain **windows.CertSimpleChain, chainCount uint32,
) ([]*windows.CertChainElement, error) {
	if simpleChain == nil || chainCount == 0 {
		return nil, errors.New("invalid simple chain")
	}
	simpleChains := unsafe.Slice(simpleChain, chainCount)
	// Each simple chain contains the chain of certificates, summary trust information
	// about the chain, and trust information about each certificate element in the chain.
	// Select the first chain.
	firstChain := simpleChains[0]
	chainLen := int(firstChain.NumElements)
	elements := unsafe.Slice(firstChain.Elements, chainLen)
	return elements, nil
}

// intendedKeyUsage wraps CertGetIntendedKeyUsage. If there are key usage bytes they will be returned,
// otherwise 0 will be returned.
func intendedKeyUsage(cert *windows.CertContext) (usage uint16) {
	_, _, _ = certGetIntendedKeyUsage.Call(uintptr(cert.EncodingType), uintptr(unsafe.Pointer(cert.CertInfo)), uintptr(unsafe.Pointer(&usage)), 2)
	return
}

// acquirePrivateKey wraps CryptAcquireCertificatePrivateKey.
func acquirePrivateKey(cert *windows.CertContext) (windows.Handle, error) {
	var (
		key      windows.Handle
		keySpec  uint32
		mustFree int
	)
	const acquireFlags = windows.CRYPT_ACQUIRE_CACHE_FLAG |
		windows.CRYPT_ACQUIRE_SILENT_FLAG |
		windows.CRYPT_ACQUIRE_ONLY_NCRYPT_KEY_FLAG
	r, _, err := cryptAcquireCertificatePrivateKey.Call(
		uintptr(unsafe.Pointer(cert)),
		acquireFlags,
		null,
		uintptr(unsafe.Pointer(&key)),
		uintptr(unsafe.Pointer(&keySpec)),
		uintptr(unsafe.Pointer(&mustFree)),
	)
	if r == 0 {
		return 0, fmt.Errorf("acquiring private key: %x %w", r, err)
	}
	if mustFree != 0 {
		return 0, fmt.Errorf("wrong mustFree [%d != 0]", mustFree)
	}
	if keySpec != windows.CERT_NCRYPT_KEY_SPEC {
		return 0, fmt.Errorf("wrong keySpec [%d != %d]", keySpec, windows.CERT_NCRYPT_KEY_SPEC)
	}
	return key, nil
}

// certChainElementsToX509 converts a slice of CertChainElement to a slice of x509.Certificate.
func certChainElementsToX509(elements []*windows.CertChainElement) ([]*x509.Certificate, error) {
	chain := make([]*x509.Certificate, 0, len(elements))
	for _, element := range elements {
		xc, err := certContextToX509(element.CertContext)
		if err != nil {
			return nil, err
		}
		chain = append(chain, xc)
	}
	return chain, nil
}

// certContextToX509 extracts the x509 certificate from the cert context.
func certContextToX509(ctx *windows.CertContext) (*x509.Certificate, error) {
	// To ensure we don't mess with the cert context's memory, use a copy of it.
	src := unsafe.Slice(ctx.EncodedCert, ctx.Length)
	der := make([]byte, int(ctx.Length))
	copy(der, src)

	xc, err := x509.ParseCertificate(der)
	if err != nil {
		return xc, err
	}
	return xc, nil
}

func certNameBlobs(names [][]byte) []windows.CertNameBlob {
	blobs := make([]windows.CertNameBlob, len(names))
	for i := range names {
		blobs[i].Size = uint32(len(names[i]))
		blobs[i].Data = &names[i][0]
	}
	return blobs
}

var errNoCertificateFound = errors.New("no matching certificate found")

// Cred returns a Key wrapping the first certificate in the system store matching one of the
// given issuerNames and satisfying the filterCallback.
func Cred(
	issuerNames [][]byte, filterCallback func(*x509.Certificate) bool,
	storeName string, provider string,
) (*Key, error) {
	var certStore uint32
	if provider == "local_machine" {
		certStore = uint32(windows.CERT_SYSTEM_STORE_LOCAL_MACHINE)
	} else if provider == "current_user" {
		certStore = uint32(windows.CERT_SYSTEM_STORE_CURRENT_USER)
	} else {
		return nil, errors.New("provider must be local_machine or current_user")
	}
	certStore |= windows.CERT_STORE_READONLY_FLAG
	storeNamePtr, err := windows.UTF16PtrFromString(storeName)
	if err != nil {
		return nil, err
	}
	store, err := windows.CertOpenStore(
		windows.CERT_STORE_PROV_SYSTEM, 0, null, certStore, uintptr(unsafe.Pointer(storeNamePtr)))
	if err != nil {
		return nil, fmt.Errorf("opening certificate store: %w", err)
	}
	var prev *windows.CertChainContext
	for {
		var para windows.CertChainFindByIssuerPara
		para.Size = uint32(unsafe.Sizeof(para))
		if issuer := certNameBlobs(issuerNames); len(issuer) > 0 {
			para.IssuerCount = uint32(len(issuer))
			para.Issuer = windows.Pointer(unsafe.Pointer(&issuer[0]))
		}
		nc, err := windows.CertFindChainInStore(
			store,
			windows.X509_ASN_ENCODING,
			windows.CERT_CHAIN_FIND_BY_ISSUER_CACHE_ONLY_FLAG,
			windows.CERT_CHAIN_FIND_BY_ISSUER,
			unsafe.Pointer(&para),
			prev,
		)
		if err != nil {
			if err == windows.Errno(windows.CRYPT_E_NOT_FOUND) {
				return nil, errNoCertificateFound
			}
			return nil, fmt.Errorf("finding certificate chains: %w", err)
		} else if nc == nil {
			return nil, errNoCertificateFound
		}
		prev = nc

		chain, err := extractSimpleChain(nc.Chains, nc.ChainCount)
		if err != nil || len(chain) == 0 {
			continue
		}

		if (intendedKeyUsage(chain[0].CertContext) & signatureKeyUsage) == 0 {
			continue
		}

		x509Chain, err := certChainElementsToX509(chain)
		if err != nil {
			continue
		}

		if !filterCallback(x509Chain[0]) {
			continue
		}

		certContext := windows.CertDuplicateCertificateContext(chain[0].CertContext)
		windows.CertFreeCertificateChain(nc)

		return newKey(x509Chain, certContext, store), nil
	}
}

// Key is a wrapper around the certificate store and context that uses it to
// implement signing-related methods with CryptoNG functionality.
type Key struct {
	cert  *x509.Certificate
	ctx   *windows.CertContext
	store windows.Handle
	chain []*x509.Certificate
	once  sync.Once
}

func newKey(
	x509Chain []*x509.Certificate,
	ctx *windows.CertContext,
	store windows.Handle,
) *Key {
	k := &Key{
		cert:  x509Chain[0],
		ctx:   ctx,
		store: store,
		chain: x509Chain,
	}
	runtime.SetFinalizer(k, func(x interface{}) {
		x.(*Key).Close()
	})
	return k
}

// CertificateChain returns the credential as a raw X509 cert chain. This
// contains the public key.
func (k *Key) CertificateChain() [][]byte {
	// Convert the certificates to a list of encoded certificate bytes.
	chain := make([][]byte, len(k.chain))
	for i, xc := range k.chain {
		chain[i] = xc.Raw
	}
	return chain
}

// Close releases resources held by the credential.
func (k *Key) Close() error {
	var result error
	k.once.Do(func() {
		if err := windows.CertFreeCertificateContext(k.ctx); err != nil {
			result = err
		}
		if err := windows.CertCloseStore(k.store, 0); err != nil {
			result = err
		}
	})
	return result
}

// Public returns the corresponding public key for this Key.
func (k *Key) Public() crypto.PublicKey {
	return k.cert.PublicKey
}

// Sign signs a message digest. Here, we pass off the signing to the Windows CryptoNG library.
func (k *Key) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	key, err := acquirePrivateKey(k.ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot acquire private key handle: %w", err)
	}
	return SignHash(key, k.Public(), digest, opts)
}
