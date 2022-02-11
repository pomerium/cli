package api_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/pomerium/cli/api"
	pb "github.com/pomerium/cli/proto"
)

func TestLoadSave(t *testing.T) {
	ctx := context.Background()

	opt := api.WithConfigProvider(new(api.MemCP))
	cfg, err := api.NewServer(ctx, opt)
	require.NoError(t, err, "load empty config")

	var ids []string
	for _, r := range []*pb.Record{
		{
			Tags: []string{"one"},
			Conn: &pb.Connection{
				Name:       proto.String("test one"),
				RemoteAddr: "test1.another.domain.com",
				ListenAddr: proto.String(":9993"),
			},
		},
		{
			Tags: []string{"one", "two"},
			Conn: &pb.Connection{
				Name:       proto.String("test two"),
				RemoteAddr: "test2.some.domain.com",
				ListenAddr: proto.String(":9991"),
			},
		},
	} {
		r, err := cfg.Upsert(ctx, r)
		if assert.NoError(t, err) {
			assert.NotNil(t, r.Id)
			ids = append(ids, r.GetId())
		}
	}

	cfg, err = api.NewServer(ctx, opt)
	require.NoError(t, err, "load config")

	selectors := map[string]*pb.Selector{
		"all": {
			All: true,
		}, "ids": {
			Ids: ids,
		}, "tags": {
			Tags: []string{"one"},
		}}
	for label, s := range selectors {
		recs, err := cfg.List(ctx, s)
		if assert.NoError(t, err, label) && assert.NotNil(t, recs, label) {
			assert.Len(t, recs.Records, len(ids), label)
		}
	}
}

func TestCertInfo(t *testing.T) {
	ctx := context.Background()

	opt := api.WithConfigProvider(new(api.MemCP))
	cfg, err := api.NewServer(ctx, opt)
	require.NoError(t, err, "load empty config")

	var ids []string
	certData := []byte(`
-----BEGIN CERTIFICATE-----
MIIEZjCCAs6gAwIBAgIQRiWdzeaOOVrkVVKw/tGtuzANBgkqhkiG9w0BAQsFADCB
iTEeMBwGA1UEChMVbWtjZXJ0IGRldmVsb3BtZW50IENBMS8wLQYDVQQLDCZkZW5p
c0BEZW5pc3MtTWFjQm9vay1Qcm8ubG9jYWwgKERlbmlzKTE2MDQGA1UEAwwtbWtj
ZXJ0IGRlbmlzQERlbmlzcy1NYWNCb29rLVByby5sb2NhbCAoRGVuaXMpMB4XDTIy
MDIwODE3MTQzNloXDTI0MDUwODE2MTQzNlowWjEnMCUGA1UEChMebWtjZXJ0IGRl
dmVsb3BtZW50IGNlcnRpZmljYXRlMS8wLQYDVQQLDCZkZW5pc0BEZW5pc3MtTWFj
Qm9vay1Qcm8ubG9jYWwgKERlbmlzKTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCC
AQoCggEBAL+hha3t9YNDQfRZv5TNKNtLsXdrFijx5q2K5c5MSiyw3EOpMmpmudsr
cPL08QFNcFCuOwixoKojs7IEiomP+XvUAlQpI3taJakbezvz2nqm2iFAqxEr4g3t
oz16pjTsQKsAMDzmVEnrp2N//gbCU1H4Z1LqhyWUN2Coz0exKBISBnjz5yjEAb9n
EeH8nw+vnLfj4hetG+tXjZhYK125ckLP19+Hfgd3cN5DzDm31FGxZl0390GOJTNK
Sut3EqAV4EviCa7OOI83B6Va1GAm3otcco9E+gyAI6TBTZBETESJONVD8d+VQCqI
Qpky7WKJ2GyB86BOnnGOR6yKsYfVXq0CAwEAAaN4MHYwDgYDVR0PAQH/BAQDAgWg
MB0GA1UdJQQWMBQGCCsGAQUFBwMCBggrBgEFBQcDATAfBgNVHSMEGDAWgBRqNl3o
CtxH9vwaIOXCqw5+xmF6ADAkBgNVHREEHTAbghl0Y3AubG9jYWxob3N0LnBvbWVy
aXVtLmlvMA0GCSqGSIb3DQEBCwUAA4IBgQBSicEZQmz6sG7mClubWS7G4VIuCsTv
T9YOybQlRIatzcGXOfvxzPGevILPcDMdb9VXd4Fxmw+XyA5G9hDG9tUkHYbI7obD
jD+F3Spqv+yFV6HLwmdYWEkV6mcEqjikTJpL3tpCr7I2GO/n4HQUK+IVRQwHa/PS
TXjydeQ87f/TlBeyoEaIDgeyewn4BxEml36E8ewa8gh+5NeW9EyFCy1gO+HLYVif
tqSQJtEbf4foONrIBtK4VOK87uJCNqilgNNsx3ZKHfgZJDoR3fc5XsyiNPjPD2/6
V6t6nPwX1ByII9ehRqS432yTcC/CBj8KLY0POh82GtC4nRKfFJhrN6EuU4h4VH3h
iYKoKjUXE2vJy+1K4qfXL4dOeLT5zbeMnpgscWyg/dzjFKCWgaoNgDN+hH0BIAfZ
tH/DVfwGHzXxlztNS1U4fOQ7Tc9ENcxuT7rfX9XI9b6UO0um2nggLdIwSsgI/fZn
UaO3jy5nXLYOlAQcLAARXE1F/onwmdNg1+I=
-----END CERTIFICATE-----
`)
	keyData := []byte(`
-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC/oYWt7fWDQ0H0
Wb+UzSjbS7F3axYo8eatiuXOTEossNxDqTJqZrnbK3Dy9PEBTXBQrjsIsaCqI7Oy
BIqJj/l71AJUKSN7WiWpG3s789p6ptohQKsRK+IN7aM9eqY07ECrADA85lRJ66dj
f/4GwlNR+GdS6ocllDdgqM9HsSgSEgZ48+coxAG/ZxHh/J8Pr5y34+IXrRvrV42Y
WCtduXJCz9ffh34Hd3DeQ8w5t9RRsWZdN/dBjiUzSkrrdxKgFeBL4gmuzjiPNwel
WtRgJt6LXHKPRPoMgCOkwU2QRExEiTjVQ/HflUAqiEKZMu1iidhsgfOgTp5xjkes
irGH1V6tAgMBAAECggEAAy5rSbfpm4RCVAwpYg8F3p4jFBhzBbr+WWM07XfWw2cs
QPNOIvYRn9HYzi/C0IK4mp5J3JsWT7zH/uyUYDRDsAqU/1Cvhhy5A1Wxdg1WUzLx
7Btmu6I+3nxTeXHP0JEbgbs/EcPBInkHDl/Rl8siFvpIvNen5rfPM4uZ5VbLk4Ex
sG/eaxEXj757GyY2VOavzwPRqjupYOIxT/jLmIfEi9zT1wwHS/N9CQy2eIxhZXzn
/Nnu6IzOWO2LkphlPp9xgWgntu0XI5hgV0qNA/W9FswbQIWURsm/Ox5l95gHLNaM
hl2WCQrxITZBTRODFuAB5RwGSN0cDdjFAfTh083YAQKBgQDDD7GkFkdVnq06r4xc
e+cPQRSGOyy/MmEJ20zD16iTt/qAnB0Ctu/Hc97LNJJQTDgp0KZUa9mGcY0D8rIu
5K1lN9gHdzCOUVPsMuyuZ9PLiqYl5yPYK5FqTjDx+UCZto+kTviDimttgdUaJa99
NOY/dljnpFicQOB+9dBhOSCz+QKBgQD7f3sAf8dZG4ocdeSeFHwmebicUdYZxUyW
DX1jcnNwgvsO0oF1rBwis2Ud5luldtBrMcOTMREdaFpV/11l2Gw3HQ9kUNaXQcQQ
OyleyoAA/oxnzZS1jpL0bbW/07ixaN0GMReSa4WMCNg2uekL2hY9Z5dOmUesyeWV
P7Hk2hPFVQKBgEZX2IYGCr+Ts4DgYcvQWukjXRVzLZXdwyTc0vglQ4PR6yKKKeQa
uKnC3WuGj+UpN2/M8M6s/gr/1AzCbwN+MBG6a8t1bitEpPEfBD947eYPIA+3JTQF
sjEV9YytiGBmd7KXUAOP3WHmWkVNpdWPSCFGupT+rX3b35mpZ/ZHtcVxAoGAHo/T
RrBAbVenZOX+ricXHyXThUt8lQ0gzWs+PYN++8Eu+RIjoUUU9jKOqx9/K5BQq3YU
qiJgTg6MS78IfoPaQqhJYotgSGk5hi9qS5aYD4bfUQ3ucFGvEfzzBSiZXRW9Ji95
CdX/GJFKlPvqkgIiibu461g9GYY/W++tkn3dwTECgYEAjVP6gzs3BuXY28z5g0MB
HyYpqfvasvBhiWnuohv/LW5qhoU58VGRM31knQeRNn3FAp3A/AEAhFdYL3KxpJez
/Uu33yBHdxahHCUo6sJg5qPKjzFPMQY6PwcvXqEfEK1P/2tV+6743FhTPs3TDkcM
KSwExwUr94Fr+qoXl/okwJY=
-----END PRIVATE KEY-----
	`)
	t.Run("add records", func(t *testing.T) {
		tc := map[*pb.Certificate]bool{}
		for _, cd := range [][]byte{nil, []byte("junk"), certData} {
			for _, kd := range [][]byte{nil, []byte("junk"), keyData} {
				valid := bytes.Equal(cd, certData) && bytes.Equal(kd, keyData)
				tc[&pb.Certificate{Cert: cd, Key: kd}] = valid
			}
		}
		for crt, valid := range tc {
			r, err := cfg.Upsert(ctx, &pb.Record{
				Tags: []string{"one"},
				Conn: &pb.Connection{
					Name:       proto.String("test one"),
					RemoteAddr: "test1.another.domain.com",
					ListenAddr: proto.String(":9993"),
					ClientCert: crt,
				},
			})
			if !valid {
				assert.Error(t, err)
				continue
			}
			if assert.NoError(t, err) {
				if assert.NotNil(t, r.Id) {
					ids = append(ids, *r.Id)
				}
			}
		}
		assert.NotEmpty(t, ids, "no records?")
	})

	cfg, err = api.NewServer(ctx, opt)
	require.NoError(t, err, "load config")

	selectors := map[string]*pb.Selector{
		"all": {
			All: true,
		}, "ids": {
			Ids: ids,
		}, "tags": {
			Tags: []string{"one"},
		}}
	t.Run("check cert info", func(t *testing.T) {
		for label, s := range selectors {
			recs, err := cfg.List(ctx, s)
			if assert.NoError(t, err, label) && assert.NotNil(t, recs, label) {
				assert.Len(t, recs.Records, len(ids), label)
				for _, rec := range recs.Records {
					if assert.NotNil(t, rec.Conn) {
						if assert.NotNil(t, rec.Conn.ClientCert) {
							assert.NotNil(t, rec.Conn.ClientCert.Info)
						}
					}
				}
			}
		}
	})
}
