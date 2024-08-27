package certstore

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issuer: C=US, O=Pomerium, OU=Engineering, OU=DevOps, CN=Test Root CA
// Subject: C=US, ST=California, L=Los Angeles, O=Pomerium, CN=Test Certificate
const testCertPEM = `-----BEGIN CERTIFICATE-----
MIIB5jCCAY2gAwIBAgICIAAwCgYIKoZIzj0EAwIwXDELMAkGA1UEBhMCVVMxETAP
BgNVBAoTCFBvbWVyaXVtMSMwDQYDVQQLEwZEZXZPcHMwEgYDVQQLEwtFbmdpbmVl
cmluZzEVMBMGA1UEAxMMVGVzdCBSb290IENBMCIYDzAwMDEwMTAxMDAwMDAwWhgP
MDAwMTAxMDEwMDAwMDBaMGYxCzAJBgNVBAYTAlVTMRMwEQYDVQQIEwpDYWxpZm9y
bmlhMRQwEgYDVQQHEwtMb3MgQW5nZWxlczERMA8GA1UEChMIUG9tZXJpdW0xGTAX
BgNVBAMTEFRlc3QgQ2VydGlmaWNhdGUwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNC
AAQgkbO2N9cx+Pu9s7FLloSxWflttjGPMv+5JqinH1VXOmxBpXEpYZ9dgWgQIFNz
onyn7SfjrYIJbTJcw+0V566+ozEwLzAMBgNVHRMBAf8EAjAAMB8GA1UdIwQYMBaA
FDxdxtd1v9iJOdiQzR9O2VbtrliRMAoGCCqGSM49BAMCA0cAMEQCIFzelZ+1njgo
wkVeQgxXi4x2ViqjsOfX6+FeyR0lhkbsAiAzcF/2EiqtTWVu6u/YXpbhIM4O7vra
5nXYEk8IjlGTDw==
-----END CERTIFICATE-----`

func TestFilterCallback(t *testing.T) {
	p, _ := pem.Decode([]byte(testCertPEM))
	cert, err := x509.ParseCertificate(p.Bytes)
	require.NoError(t, err)

	cases := []struct {
		label         string
		issuerFilter  string
		subjectFilter string
		match         bool
	}{
		{"no filter", "", "", true},
		{"issuer CN match", "CN=Test Root CA", "", true},
		{"issuer CN no match", "CN=Test Certificate", "", false},
		{"subject ST match", "", "ST=California", true},
		{"subject ST no match", "", "ST=New York", false},
		{"issuer and subject match", "CN=Test Root CA", "CN=Test Certificate", true},
		{"issuer and subject swapped", "CN=Test Certificate", "CN=Test Root CA", false},
		{"full attribute names", "organizationName=Pomerium", "localityName=Los Angeles", true},
		{"case insensitive attribute names", "o=Pomerium", "LOCALITYNAME=Los Angeles", true},
		{"case sensitive values", "o=pomerium", "l=los angeles", false},
		{"one of multiple attribute values/1", "OU=Engineering", "", true},
		{"one of multiple attribute values/2", "OU=DevOps", "", true},
	}
	for i := range cases {
		c := &cases[i]
		t.Run(c.label, func(t *testing.T) {
			f, err := filterCallback(c.issuerFilter, c.subjectFilter)
			require.NoError(t, err)
			assert.Equal(t, c.match, f(cert))
		})
	}
}

func TestParseFilterCondition(t *testing.T) {
	cases := []struct {
		label  string
		input  string
		attr   string
		value  string
		errMsg string
	}{
		{"empty", "", "", "", ""},
		{"invalid", "foo", "foo", "", `expected filter format attr=value, but was "foo"`},
		{"unknown", "foo=bar", "foo", "bar", `unsupported attribute "foo"`},
		{"ok", "cn=some name", "cn", "some name", ""},
	}
	for i := range cases {
		c := &cases[i]
		t.Run(c.label, func(t *testing.T) {
			attr, value, err := parseFilterCondition(c.input)
			assert.Equal(t, c.attr, attr)
			assert.Equal(t, c.value, value)
			if c.errMsg == "" {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, c.errMsg, err.Error())
			}
		})
	}
}

func TestAttributeLookup(t *testing.T) {
	name := &pkix.Name{
		Country:            []string{"Italia"},
		Organization:       []string{"Pomerium"},
		OrganizationalUnit: []string{"Engineering", "DevOps"},
		Locality:           []string{"Tivoli"},
		Province:           []string{"Roma"},
		StreetAddress:      []string{"Via Esempio 123"},
		PostalCode:         []string{"12345"},
		SerialNumber:       "67890",
		CommonName:         "common name",
	}

	cases := []struct {
		attr  string
		value []string
	}{
		{"c", []string{"Italia"}},
		{"countryname", []string{"Italia"}},
		{"o", []string{"Pomerium"}},
		{"organizationname", []string{"Pomerium"}},
		{"ou", []string{"Engineering", "DevOps"}},
		{"organizationalunitname", []string{"Engineering", "DevOps"}},
		{"l", []string{"Tivoli"}},
		{"localityname", []string{"Tivoli"}},
		{"st", []string{"Roma"}},
		{"stateorprovincename", []string{"Roma"}},
		{"street", []string{"Via Esempio 123"}},
		{"streetaddress", []string{"Via Esempio 123"}},
		{"postalcode", []string{"12345"}},
		{"serialnumber", []string{"67890"}},
	}
	for i := range cases {
		c := &cases[i]
		t.Run(c.attr, func(t *testing.T) {
			value, err := attributeLookup(name, c.attr)
			require.NoError(t, err)
			assert.Equal(t, c.value, value)
		})
	}
}
