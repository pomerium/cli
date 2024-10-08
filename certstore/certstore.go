// Package certstore handles loading client certificates and private keys from
// an OS-specific certificate store.
package certstore

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"slices"
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
			if err != nil || !slices.Contains(v, issuerValue) {
				return false
			}
		}
		if subjectAttr != "" {
			v, err := attributeLookup(&cert.Subject, subjectAttr)
			if err != nil || !slices.Contains(v, subjectValue) {
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

// attributeLookup returns an attribute's value(s) from a pkix.Name struct.
// Attributes and abbreviations are defined in RFC 2256 § 5. Only the named
// fields of pkix.Name are supported.
func attributeLookup(name *pkix.Name, attr string) ([]string, error) {
	switch attr {
	case "commonname", "cn":
		return []string{name.CommonName}, nil
	case "countryname", "c":
		return name.Country, nil
	case "localityname", "l":
		return name.Locality, nil
	case "organizationalunitname", "ou":
		return name.OrganizationalUnit, nil
	case "organizationname", "o":
		return name.Organization, nil
	case "postalcode":
		return name.PostalCode, nil
	case "serialnumber":
		return []string{name.SerialNumber}, nil
	case "stateorprovincename", "st":
		return name.Province, nil
	case "streetaddress", "street":
		return name.StreetAddress, nil
	default:
		return nil, fmt.Errorf("unsupported attribute %q", attr)
	}
}
