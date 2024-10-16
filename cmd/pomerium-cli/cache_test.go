package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCachedCredentialPath(t *testing.T) {
	Testservers := []struct {
		Name      string
		ServerURL string
	}{
		{
			Name:      "Oauth2 Server",
			ServerURL: "https://oauth.example.com/token",
		},
		{
			Name:      "LADP server",
			ServerURL: "ldap://ldap.example.com",
		},
		{
			Name:      "Active Directory Server",
			ServerURL: "ldap://ad.example.com",
		},
		{
			Name:      "SAML Authentication Server",
			ServerURL: "https://saml.example.com/auth",
		},
		{
			Name:      "OpenID Connect Server",
			ServerURL: "https://openid.example.com/auth",
		},
		{
			Name:      "JWT Authentication Server",
			ServerURL: "https://jwt.example.com/auth",
		},
	}
	for _, tc := range Testservers {
		t.Run(tc.Name, func(t *testing.T) {
			actualfileStr, err := cachedCredentialPath(tc.ServerURL)
			assert.NoError(t, err)
			assert.NotNil(t, actualfileStr)
		})
	}
}

func TestClearCachedCredential(t *testing.T) {
	filepath := t.TempDir()
	testCases := []struct {
		Name      string
		ServerURL string
		CredFile  string
	}{
		{
			Name:      "Token Authentication Server",
			ServerURL: "https://auth.example.com/token",
			CredFile:  "/pomerium-cli/exec-credential/9a5e6376e10c7336bf32ad0be076c12909c44dd460c13110cf0c9b76eab85d51.json",
		},
		{
			Name:      "Oauth1 Server",
			ServerURL: "https://oauth1.example.com/access_token",
			CredFile:  "/pomerium-cli/exec-credential/8cf70b03e5a6986f71a885f91bdf4e55e787b90a04457e05c7a8d02e6ff38163.json",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			err := clearCachedCredential(tc.ServerURL)
			if err != nil {
				t.Log("unable to clear cached credentials:", err)
				return
			}
			//ensure cachePath are cleared
			dir, err := os.Open(filepath + tc.CredFile)
			assert.NoError(t, err)
			defer dir.Close()

			files, err := dir.ReadDir(0)
			assert.Error(t, err)
			assert.Equal(t, 0, len(files))
		})

	}
}
