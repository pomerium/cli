// Package certstore handles loading client certificates and private keys from
// an OS-specific certificate store.
package certstore

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"strings"
)

var errNotSupported = errors.New("this build of pomerium-cli does not support this feature")

// GetClientCertificateFunc returns a function suitable for use as a
// [tls.Config.GetClientCertificate] callback. This function searches for a
// client certificate in the system trust store according to the list of
// acceptable CA names from the Certificate Request message, with optional
// additional filter conditions based on the Issuer name and/or the Subject
// name in the end-entity certificate.
//
// Filter conditions should be of the form "attribute=value", e.g. "CN=my cert
// name". Each condition may include at most one attribute/value pair. Only
// attributes corresponding to named fields of [pkix.Name] may be used
// (attribute keys are compared case-insensitively). These attributes are:
//   - commonName (CN)
//   - countryName (C)
//   - localityName (L)
//   - organizationName (O)
//   - organizationalUnitName (OU)
//   - postalCode
//   - serialNumber
//   - stateOrProvinceName (ST)
//   - streetAddress (STREET)
//
// Names containing multiple values for the same attribute are not supported.
func GetClientCertificateFunc(
	issuerFilter, subjectFilter string,
) (func(*tls.CertificateRequestInfo) (*tls.Certificate, error), error) {
	if !IsCertstoreSupported {
		return nil, errNotSupported
	}

	f, err := filterCallback(issuerFilter, subjectFilter)
	if err != nil {
		return nil, err
	}

	return func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
		return loadCert(cri.AcceptableCAs, f)
	}, nil
}

func filterCallback(issuerFilter, subjectFilter string) (func(*x509.Certificate) bool, error) {
	issuerAttr, issuerValue, err := parseFilterCondition(issuerFilter)
	if err != nil {
		return nil, err
	}
	subjectAttr, subjectValue, err := parseFilterCondition(subjectFilter)
	if err != nil {
		return nil, err
	}

	return func(cert *x509.Certificate) bool {
		if issuerAttr != "" {
			v, err := attributeLookup(&cert.Issuer, issuerAttr)
			if err != nil || v != issuerValue {
				return false
			}
		}
		if subjectAttr != "" {
			v, err := attributeLookup(&cert.Subject, subjectAttr)
			if err != nil || v != subjectValue {
				return false
			}
		}
		return true
	}, nil
}

func parseFilterCondition(f string) (attr, value string, err error) {
	if f == "" {
		return
	}

	var ok bool
	attr, value, ok = strings.Cut(f, "=")
	if !ok {
		err = fmt.Errorf("expected filter format attr=value, but was %q", f)
		return
	}

	attr = strings.ToLower(attr)

	// Make sure the attribute name is one we support.
	_, err = attributeLookup(&pkix.Name{}, attr)
	return
}

// attributeLookup returns a single attribute value from a pkix.Name struct.
// Multi-valued RDNs are not supported. Attributes and abbreviations are
// defined in RFC 2256 § 5. Only the named fields of pkix.Name are supported.
func attributeLookup(name *pkix.Name, attr string) (string, error) {
	switch attr {
	case "commonname", "cn":
		return name.CommonName, nil
	case "countryname", "c":
		return flatten(name.Country)
	case "localityname", "l":
		return flatten(name.Locality)
	case "organizationalunitname", "ou":
		return flatten(name.OrganizationalUnit)
	case "organizationname", "o":
		return flatten(name.Organization)
	case "postalcode":
		return flatten(name.PostalCode)
	case "serialnumber":
		return name.SerialNumber, nil
	case "stateorprovincename", "st":
		return flatten(name.Province)
	case "streetaddress", "street":
		return flatten(name.StreetAddress)
	default:
		return "", fmt.Errorf("unsupported attribute %q", attr)
	}
}

func flatten(s []string) (string, error) {
	if len(s) > 1 {
		return "", fmt.Errorf("multi-valued attributes are not supported")
	}
	if len(s) == 0 {
		return "", nil
	}
	return s[0], nil
}
