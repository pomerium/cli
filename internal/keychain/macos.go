//go:build darwin && cgo
// +build darwin,cgo

package keychain

/*
#cgo LDFLAGS: -framework CoreFoundation -framework Security

#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>

CFDictionaryRef CFDictionaryCreateSafe(CFAllocatorRef allocator, const uintptr_t *keys, const uintptr_t *values, CFIndex numValues, const CFDictionaryKeyCallBacks *keyCallBacks, const CFDictionaryValueCallBacks *valueCallBacks) {
  return CFDictionaryCreate(allocator, (const void **)keys, (const void **)values, numValues, keyCallBacks, valueCallBacks);
}

CFArrayRef CFArrayCreateSafe(CFAllocatorRef allocator, const uintptr_t *values, CFIndex numValues, const CFArrayCallBacks *callBacks) {
  return CFArrayCreate(allocator, (const void **)values, numValues, callBacks);
}

*/
import "C"

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"unsafe"
)

// XXX
type keychainIdentity struct {
	certificateRaw []byte
	certificate    *x509.Certificate

	key C.SecKeyRef
}

func newKeychainIdentity(identity C.SecIdentityRef) (*keychainIdentity, error) {
	// Extract certificate.
	var c C.SecCertificateRef
	if s := C.SecIdentityCopyCertificate(identity, &c); s != C.errSecSuccess {
		return nil, toGoError(s)
	}
	defer C.CFRelease(C.CFTypeRef(c))
	d := C.SecCertificateCopyData(c)
	defer C.CFRelease(C.CFTypeRef(d))
	certRaw := toGoBytes(d)
	cert, err := x509.ParseCertificate(certRaw)
	if err != nil {
		return nil, err
	}

	// Store private key reference.
	var key C.SecKeyRef
	if s := C.SecIdentityCopyPrivateKey(identity, &key); s != C.errSecSuccess {
		return nil, toGoError(s)
	}

	return &keychainIdentity{
		certificateRaw: certRaw,
		certificate:    cert,
		key:            key,
	}, nil
}

func (i *keychainIdentity) release() {
	C.CFRelease(C.CFTypeRef(i.key))
}

func (i *keychainIdentity) String() string {
	keyType := "unknown type"
	switch k := i.certificate.PublicKey.(type) {
	case *rsa.PublicKey:
		keyType = fmt.Sprintf("RSA (%d bits)", k.N.BitLen())
	case *ecdsa.PublicKey:
		keyType = fmt.Sprintf("ECDSA (%s)", k.Curve.Params().Name)
	}
	return fmt.Sprintf("%s - %s", i.certificate.Subject.String(), keyType)
}

func (i *keychainIdentity) Public() crypto.PublicKey {
	return i.certificate.PublicKey
}

func (i *keychainIdentity) Sign(
	rand io.Reader, digest []byte, opts crypto.SignerOpts,
) (signature []byte, err error) {

	fmt.Println("XXX - keychainIdentity.Sign():", hex.EncodeToString(digest), opts)

	alg := i.algorithm(opts)
	if alg == nullCFStringRef {
		return nil, errors.New("no supported signing algorithm")
	}

	cfDigest, r, err := toCFData(digest)
	if err != nil {
		return nil, err
	}
	defer r()

	var cfErr C.CFErrorRef
	sig := C.SecKeyCreateSignature(i.key, alg, cfDigest, &cfErr)

	if cfErr != 0 {
		return nil, errors.New("couldn't sign") // XXX: translate error; release error?
	}

	return toGoBytes(sig), nil
}

var nullCFStringRef C.CFStringRef

func (i *keychainIdentity) algorithm(opts crypto.SignerOpts) C.SecKeyAlgorithm {
	switch i.Public().(type) {
	case *rsa.PublicKey:
		return rsaAlgorithm(opts)
	case *ecdsa.PublicKey:
		return ecdsaAlgorithm(opts)
	}
	return nullCFStringRef
}

func rsaAlgorithm(opts crypto.SignerOpts) C.SecKeyAlgorithm {
	switch o := opts.(type) {
	case *rsa.PSSOptions:
		switch o.Hash {
		case crypto.SHA224:
			return C.kSecKeyAlgorithmRSASignatureDigestPSSSHA224
		case crypto.SHA256:
			return C.kSecKeyAlgorithmRSASignatureDigestPSSSHA256
		case crypto.SHA384:
			return C.kSecKeyAlgorithmRSASignatureDigestPSSSHA384
		case crypto.SHA512:
			return C.kSecKeyAlgorithmRSASignatureDigestPSSSHA512
		}
	case crypto.Hash:
		switch o {
		case crypto.SHA224:
			return C.kSecKeyAlgorithmRSASignatureDigestPKCS1v15SHA224
		case crypto.SHA256:
			return C.kSecKeyAlgorithmRSASignatureDigestPKCS1v15SHA256
		case crypto.SHA384:
			return C.kSecKeyAlgorithmRSASignatureDigestPKCS1v15SHA384
		case crypto.SHA512:
			return C.kSecKeyAlgorithmRSASignatureDigestPKCS1v15SHA512
		}
	}

	return nullCFStringRef
}

func ecdsaAlgorithm(opts crypto.SignerOpts) C.SecKeyAlgorithm {
	// XXX: I'm not at all sure this is right... there's a set of X9.62 signature
	// algorithms and a set of RFC4754 algorithms... the RFC4754 ones are marked
	// beta though, so we probably want the X9.62 ones?

	hash, ok := opts.(crypto.Hash)
	if !ok {
		return nullCFStringRef
	}

	switch hash {
	case crypto.SHA224:
		return C.kSecKeyAlgorithmECDSASignatureDigestX962SHA224
	case crypto.SHA256:
		return C.kSecKeyAlgorithmECDSASignatureDigestX962SHA256
	case crypto.SHA384:
		return C.kSecKeyAlgorithmECDSASignatureDigestX962SHA384
	case crypto.SHA512:
		return C.kSecKeyAlgorithmECDSASignatureDigestX962SHA512
	}

	return nullCFStringRef
}

func loadClientCertificates(commonName, organizationalUnit string) ([]tls.Certificate, error) {
	item, release, err := toCFDictionary(map[string]any{
		//toGoString(C.kSecReturnData): true,
		toGoString(C.kSecClass):      toGoString(C.kSecClassIdentity),
		toGoString(C.kSecMatchLimit): toGoString(C.kSecMatchLimitAll),
	})
	if err != nil {
		return nil, err
	}
	defer release()

	var matches C.CFTypeRef
	status := C.SecItemCopyMatching(item, &matches)
	if status != C.errSecSuccess {
		return nil, fmt.Errorf("error loading items from keychain: %d", status)
	}
	defer C.CFRelease(matches) // XXX: does CFRelease release items in an array?

	var tlsCertificates []tls.Certificate
	matchesArray := C.CFArrayRef(matches)

	n := int(C.CFArrayGetCount(matchesArray))
	for i := 0; i < n; i++ {
		// XXX - can we get identity name with kSecLabelItemAttr?
		//       looks like we would need to set kSecReturnAttributes and then parse a dictionary?
		identity, err := newKeychainIdentity(C.SecIdentityRef(
			C.CFArrayGetValueAtIndex(matchesArray, C.CFIndex(i))))
		if err != nil {
			log.Println("error loading identity:", err)
			continue
		}
		fmt.Printf(" * identity %d: %v\n", i, identity)
		tlsCertificates = append(tlsCertificates, tls.Certificate{
			Certificate: [][]byte{identity.certificateRaw},
			PrivateKey:  identity,
		})
	}

	if len(tlsCertificates) == 0 {
		return nil, fmt.Errorf("could not load any identities")
	}

	return tlsCertificates, nil

	// XXX: need to call release() on all identities
	// identity.release()
}

func toGo(v any) any {
	if ref, ok := v.(C.CFTypeRef); ok {
		typeID := C.CFGetTypeID(ref)
		//fmt.Println("XXX - ", typeID)
		switch typeID {
		case C.CFStringGetTypeID():
			v = C.CFStringRef(ref)
		case C.CFArrayGetTypeID():
			v = C.CFArrayRef(ref)
		case C.CFDataGetTypeID():
			v = C.CFDataRef(ref)
		case C.SecIdentityGetTypeID():
			v = C.SecIdentityRef(ref)
		}
	}

	switch v := v.(type) {
	case C.CFStringRef:
		return toGoString(v)
	case C.CFArrayRef:
		return toGoSlice(v)
	case C.CFDataRef:
		return toGoBytes(v)
	}

	panic(fmt.Errorf("unknown value type: %T", v))
}

func toGoError(status C.OSStatus) error {
	s := C.SecCopyErrorMessageString(status, nil)
	defer C.CFRelease(C.CFTypeRef(s))
	return errors.New(toGoString(s))
}

func toGoSlice(v C.CFArrayRef) []any {
	n := int(C.CFArrayGetCount(v))
	s := make([]any, n)
	for i := 0; i < n; i++ {
		s[i] = toGo(C.CFArrayGetValueAtIndex(v, C.CFIndex(i)))
	}
	return s
}

func toGoString(v C.CFStringRef) string {
	n := C.CFStringGetLength(v)
	if n == 0 {
		return ""
	}
	max := C.CFStringGetMaximumSizeForEncoding(n, C.kCFStringEncodingUTF8)
	if max == 0 {
		return ""
	}
	buf := make([]byte, max)
	var used C.CFIndex
	_ = C.CFStringGetBytes(v, C.CFRange{0, n}, C.kCFStringEncodingUTF8, C.UInt8(0), C.false, (*C.UInt8)(&buf[0]), max, &used)
	return string(buf[:used])
}

func toGoBytes(v C.CFDataRef) []byte {
	return C.GoBytes(unsafe.Pointer(C.CFDataGetBytePtr(v)), C.int(C.CFDataGetLength(v)))
}

func toCF(v any) (C.CFTypeRef, func(), error) {
	switch v := v.(type) {
	case []any:
		p, r, err := toCFArray(v)
		return C.CFTypeRef(p), r, err
	case bool:
		p, r, err := toCFBool(v)
		return C.CFTypeRef(p), r, err
	case []byte:
		p, r, err := toCFData(v)
		return C.CFTypeRef(p), r, err
	case map[string]any:
		p, r, err := toCFDictionary(v)
		return C.CFTypeRef(p), r, err
	case string:
		p, r, err := toCFString(v)
		return C.CFTypeRef(p), r, err
	}

	return 0, nil, fmt.Errorf("unknown value type: %T", v)
}

func toCFArray(arr []any) (C.CFArrayRef, func(), error) {
	var vsPtr *C.uintptr_t
	n := len(arr)
	if n > 0 {
		vs := make([]C.uintptr_t, 0, n)
		for _, v := range arr {
			cfV, cfVRelease, err := toCF(v)
			if err != nil {
				return 0, nil, err
			}
			defer cfVRelease()

			vs = append(vs, C.uintptr_t(cfV))
		}
		vsPtr = &vs[0]
	}

	ref := C.CFArrayCreateSafe(C.kCFAllocatorDefault, vsPtr, C.CFIndex(n), &C.kCFTypeArrayCallBacks)
	if ref == 0 {
		return 0, nil, fmt.Errorf("error creating CFArray")
	}
	return ref, func() { C.CFRelease(C.CFTypeRef(ref)) }, nil
}

func toCFBool(v bool) (C.CFBooleanRef, func(), error) {
	if v {
		return C.kCFBooleanTrue, func() {}, nil
	}
	return C.kCFBooleanFalse, func() {}, nil
}

func toCFData(v []byte) (C.CFDataRef, func(), error) {
	var ptr *C.UInt8
	if len(v) > 0 {
		bs := []byte(v)
		ptr = (*C.UInt8)(&bs[0])
	}
	ref := C.CFDataCreate(C.kCFAllocatorDefault, ptr, C.CFIndex(len(v)))
	if ref == 0 {
		return ref, nil, fmt.Errorf("error creating CFData")
	}
	return ref, func() { C.CFRelease(C.CFTypeRef(ref)) }, nil
}

func toCFDictionary(v map[string]any) (C.CFDictionaryRef, func(), error) {
	var ksPtr, vsPtr *C.uintptr_t
	n := len(v)
	if n > 0 {
		ks := make([]C.uintptr_t, 0, n)
		vs := make([]C.uintptr_t, 0, n)
		for k, v := range v {
			cfK, cfKRelease, err := toCF(k)
			if err != nil {
				return 0, nil, err
			}
			defer cfKRelease()

			cfV, cfVRelease, err := toCF(v)
			if err != nil {
				return 0, nil, err
			}
			defer cfVRelease()

			ks = append(ks, C.uintptr_t(cfK))
			vs = append(vs, C.uintptr_t(cfV))
		}
		ksPtr = &ks[0]
		vsPtr = &vs[0]
	}

	ref := C.CFDictionaryCreateSafe(
		C.kCFAllocatorDefault,
		ksPtr, vsPtr,
		C.CFIndex(n),
		&C.kCFTypeDictionaryKeyCallBacks, &C.kCFTypeDictionaryValueCallBacks,
	)
	if ref == 0 {
		return ref, nil, fmt.Errorf("CFDictionaryCreate failed")
	}
	return ref, func() { C.CFRelease(C.CFTypeRef(ref)) }, nil
}

func toCFString(v string) (C.CFStringRef, func(), error) {
	var ptr *C.UInt8
	if len(v) > 0 {
		bs := []byte(v)
		ptr = (*C.UInt8)(&bs[0])
	}
	ref := C.CFStringCreateWithBytes(C.kCFAllocatorDefault, ptr, C.CFIndex(len(v)), C.kCFStringEncodingUTF8, C.false)
	if ref == 0 {
		return ref, nil, fmt.Errorf("error creating CFString")
	}
	return ref, func() { C.CFRelease(C.CFTypeRef(ref)) }, nil
}
