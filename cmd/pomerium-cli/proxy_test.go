package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTCPTunnel(t *testing.T) {
	tests := []struct {
		Name                string
		DstHost             string
		SpecificPomeriumURL string
		ExpectedError       string
	}{
		{
			Name:                "Valid URL",
			DstHost:             "example.com:443",
			SpecificPomeriumURL: "https://pomerium.example.com",
			ExpectedError:       "",
		},
		{
			Name:                "Invalid Destination",
			DstHost:             "invalid-host",
			SpecificPomeriumURL: "https://pomerium.example.com",
			ExpectedError:       "invalid destination",
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			tunnel, err := newTCPTunnel(tc.DstHost, tc.SpecificPomeriumURL)

			if tc.ExpectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.ExpectedError)
				assert.Nil(t, tunnel)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, tunnel)
			}

		})
	}
}
