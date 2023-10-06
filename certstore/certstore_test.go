package certstore

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issuer: C=US, O=Pomerium, OU=Engineering, CN=Test Root CA
// Subject: C=US, ST=California, L=Los Angeles, O=Pomerium, CN=Test Certificate
const testCertPEM = `-----BEGIN CERTIFICATE-----
MIIB7jCCAZOgAwIBAgICIAAwCgYIKoZIzj0EAwIwTTELMAkGA1UEBhMCVVMxETAP
BgNVBAoTCFBvbWVyaXVtMRQwEgYDVQQLEwtFbmdpbmVlcmluZzEVMBMGA1UEAxMM
VGVzdCBSb290IENBMCIYDzAwMDEwMTAxMDAwMDAwWhgPMDAwMTAxMDEwMDAwMDBa
MGYxCzAJBgNVBAYTAlVTMRMwEQYDVQQIEwpDYWxpZm9ybmlhMRQwEgYDVQQHEwtM
b3MgQW5nZWxlczERMA8GA1UEChMIUG9tZXJpdW0xGTAXBgNVBAMTEFRlc3QgQ2Vy
dGlmaWNhdGUwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAASwx43T5tT/gvl0MjOZ
pRMvDs2L6HqcN4vNmsbJRk/sTQrD0xVd4kzZc8mW7Q0/3WfE6QwbqkEyvxaPJ0iA
8Xhvo0YwRDATBgNVHSUEDDAKBggrBgEFBQcDAjAMBgNVHRMBAf8EAjAAMB8GA1Ud
IwQYMBaAFFGNnzA+46PbyMi0anD7+kfQInDQMAoGCCqGSM49BAMCA0kAMEYCIQCc
31d4ncyipKvvF/sDAb43lAcwXHh3d+J68RoGDEBaAwIhAM8zV4cVob9hkh6oxb61
q/MkLGpAvT+8J0K+JmvvCfTe
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
		OrganizationalUnit: []string{"Engineering"},
		Locality:           []string{"Tivoli"},
		Province:           []string{"Roma"},
		StreetAddress:      []string{"Via Esempio 123"},
		PostalCode:         []string{"12345"},
		SerialNumber:       "67890",
		CommonName:         "common name",
	}

	cases := []struct {
		attr  string
		value string
	}{
		{"c", "Italia"},
		{"countryname", "Italia"},
		{"o", "Pomerium"},
		{"organizationname", "Pomerium"},
		{"ou", "Engineering"},
		{"organizationalunitname", "Engineering"},
		{"l", "Tivoli"},
		{"localityname", "Tivoli"},
		{"st", "Roma"},
		{"stateorprovincename", "Roma"},
		{"street", "Via Esempio 123"},
		{"streetaddress", "Via Esempio 123"},
		{"postalcode", "12345"},
		{"serialnumber", "67890"},
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
