package proto

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"net"
	"net/url"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// NewCertInfo extracts certificate info to the protobuf format
func NewCertInfo(cert *x509.Certificate) *CertificateInfo {
	return &CertificateInfo{
		Version:                     int64(cert.Version),
		Serial:                      cert.SerialNumber.String(),
		Issuer:                      nameToPb(cert.Issuer),
		Subject:                     nameToPb(cert.Subject),
		NotBefore:                   timestamppb.New(cert.NotBefore),
		NotAfter:                    timestamppb.New(cert.NotAfter),
		KeyUsage:                    keyUsage(cert.KeyUsage, cert.ExtKeyUsage),
		DnsNames:                    cert.DNSNames,
		EmailAddresses:              cert.EmailAddresses,
		IpAddresses:                 ipToStrings(cert.IPAddresses),
		Uris:                        urlsToStrings(cert.URIs),
		PermittedDnsDomainsCritical: cert.PermittedDNSDomainsCritical,
		PermittedDnsDomains:         cert.PermittedDNSDomains,
		ExcludedDnsDomains:          cert.ExcludedDNSDomains,
		PermittedIpRanges:           ipNetToStrings(cert.PermittedIPRanges),
		ExcludedEmailAddresses:      cert.ExcludedEmailAddresses,
		PermittedUriDomains:         cert.PermittedURIDomains,
		ExcludedUriDomains:          cert.ExcludedURIDomains,
	}
}

func nameToPb(src pkix.Name) *Name {
	return &Name{
		Country:            src.Country,
		Organization:       src.Organization,
		OrganizationalUnit: src.OrganizationalUnit,
		Locality:           src.Locality,
		Province:           src.Province,
		StreetAddress:      src.StreetAddress,
		PostalCode:         src.PostalCode,
		SerialNumber:       src.SerialNumber,
		CommonName:         src.CommonName,
	}
}

func keyUsage(src x509.KeyUsage, ext []x509.ExtKeyUsage) *KeyUsage {
	usage := &KeyUsage{
		DigitalSignature:  src&x509.KeyUsageDigitalSignature != 0,
		ContentCommitment: src&x509.KeyUsageContentCommitment != 0,
		KeyEncipherment:   src&x509.KeyUsageDataEncipherment != 0,
		DataEncipherment:  src&x509.KeyUsageDataEncipherment != 0,
		KeyAgreement:      src&x509.KeyUsageKeyAgreement != 0,
		CertSign:          src&x509.KeyUsageCertSign != 0,
		CrlSign:           src&x509.KeyUsageCRLSign != 0,
		EncipherOnly:      src&x509.KeyUsageEncipherOnly != 0,
		DecipherOnly:      src&x509.KeyUsageDecipherOnly != 0,
	}
	for _, u := range ext {
		switch u {
		case x509.ExtKeyUsageClientAuth:
			usage.ClientAuth = true
		case x509.ExtKeyUsageServerAuth:
			usage.ServerAuth = true
		}
	}
	return usage
}

func urlsToStrings(src []*url.URL) []string {
	out := make([]string, 0, len(src))
	for _, u := range src {
		out = append(out, u.String())
	}
	return out
}

func ipToStrings(src []net.IP) []string {
	out := make([]string, 0, len(src))
	for _, u := range src {
		out = append(out, u.String())
	}
	return out
}

func ipNetToStrings(src []*net.IPNet) []string {
	out := make([]string, 0, len(src))
	for _, u := range src {
		out = append(out, u.String())
	}
	return out
}
