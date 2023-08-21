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
	"crypto/tls"
	"fmt"
)

func loadClientCertificates(commonName, organizationalUnit string) ([]tls.Certificate, error) {
	item, release, err := toCFDictionary(map[string]any{
		toGoString(C.kSecReturnData): true,
		toGoString(C.kSecClass):      toGoString(C.kSecClassIdentity),
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
	defer C.CFRelease(matches)

	fmt.Println(toGo(matches))

	panic("!!!!")
}

func toGo(v any) any {
	switch v := v.(type) {
	case C.CFStringRef:
		return toGoString(v)
	case C.CFArrayRef:
		return toGoSlice(v)
	}

	panic(fmt.Errorf("unknown value type: %T", v))
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
