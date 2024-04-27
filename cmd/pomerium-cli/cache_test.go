package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// creating Mock Cache operations
type MockCache struct {
	mock.Mock
}

// mock func for cache.ExecCredentialsPath()
func (m *MockCache) ExecCredentialsPath() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func TestClearAllCachedCredentials(t *testing.T) {
	filePath := t.TempDir()
	//mock object
	mockCache := new(MockCache)

	//set up dir expectation for ExecCredentialsPath
	mockCache.On(" ExecCredentialsPath").Return(filePath+"/tmp/cache", nil)
	_, err := os.Create(filePath + "/tmp/cache/file1.json")
	if err != nil {
		t.Log(err)
		return
	}
	_, err = os.Create(filePath + "/tmp/cache/file2.json")
	if err != nil {
		t.Log(err)
		return
	}

	err = clearAllCachedCredentials()
	if err != nil {
		t.Log(err)
		return
	}

	// Verify that the cache directory is empty
	dir, err := os.Open(filePath + "/tmp/cache")
	assert.NoError(t, err)
	defer dir.Close()

	files, err := dir.Readdir(0)
	assert.Error(t, err)
	assert.Equal(t, 0, len(files))
}

func TestCachedCredentialPath(t *testing.T) {
	//machine filepath name declared when running tests
	pcPath := "/home/dell/.cache"
	Testservers := []struct {
		Name            string
		ServerURL       string
		expectedfileStr string
	}{
		{
			Name:            "Oauth2 Server",
			ServerURL:       "https://oauth.example.com/token",
			expectedfileStr: fmt.Sprintf("%s/pomerium-cli/exec-credential/69f5783b7ea001d28057a878dabcd152dbb53d8f4a4292d627fccb37d0a599c9.json", pcPath),
		},
		{
			Name:            "LADP server",
			ServerURL:       "ldap://ldap.example.com",
			expectedfileStr: fmt.Sprintf("%s/pomerium-cli/exec-credential/7395f94f936821c8762d36f9e2a2f9d08019615d902fbbab66734e865bc7ffdb.json", pcPath),
		},
		{
			Name:            "Active Directory Server",
			ServerURL:       "ldap://ad.example.com",
			expectedfileStr: fmt.Sprintf("%s/pomerium-cli/exec-credential/14f8a8115a66dcf32c1f1cd19afc1e3ac067355cf49ffe7c9b4070f0981cfeb6.json", pcPath),
		},
		{
			Name:            "SAML Authentication Server",
			ServerURL:       "https://saml.example.com/auth",
			expectedfileStr: fmt.Sprintf("%s/pomerium-cli/exec-credential/63e6533a0b622bbf900374505a0cfc72a4316687118e61a5f379ae0518a8cf84.json", pcPath),
		},
		{
			Name:            "OpenID Connect Server",
			ServerURL:       "https://openid.example.com/auth",
			expectedfileStr: fmt.Sprintf("%s/pomerium-cli/exec-credential/d43f430a0d22f9fa76aca8d30d02b4d93050f8f4031835dd67efe28ddccd80c0.json", pcPath),
		},
		{
			Name:            "JWT Authentication Server",
			ServerURL:       "https://jwt.example.com/auth",
			expectedfileStr: fmt.Sprintf("%s/pomerium-cli/exec-credential/b0b5d8589c64e2b7d5d7646fb9f0464d40196eb203efd37fda5e02d57cbaf5f5.json", pcPath),
		},
	}
	for _, tc := range Testservers {
		t.Run(tc.Name, func(t *testing.T) {
			actualfileStr, err := cachedCredentialPath(tc.ServerURL)
			if err != nil {
				t.Log(err)
				return
			}
			assert.NotNil(t, actualfileStr)
			assert.Equal(t, tc.expectedfileStr, actualfileStr)
		})
	}
}

func TestClearCachedCredential(t *testing.T) {
	pcPath := "/home/dell/.cache"
	filepath := t.TempDir()
	testCases := []struct {
		Name      string
		ServerURL string
		CredFile  string
	}{
		{
			Name:      "Token Authentication Server",
			ServerURL: "https://auth.example.com/token",
			CredFile:  filepath + fmt.Sprintf("%s/pomerium-cli/exec-credential/9a5e6376e10c7336bf32ad0be076c12909c44dd460c13110cf0c9b76eab85d51.json", pcPath),
		},
		{
			Name:      "Oauth1 Server",
			ServerURL: "https://oauth1.example.com/access_token",
			CredFile:  filepath + fmt.Sprintf("%s/pomerium-cli/exec-credential/8cf70b03e5a6986f71a885f91bdf4e55e787b90a04457e05c7a8d02e6ff38163.json", pcPath),
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
