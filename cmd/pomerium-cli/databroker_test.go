package main

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDBCmdParse(t *testing.T) {
	cmd := &dbCmd{
		serviceURL: "https://example.com:8443",
	}
	err := cmd.parse(nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, "databroker", cmd.ServiceName)
	expectedURL, _ := url.Parse("https://example.com:8443")
	assert.Equal(t, expectedURL, cmd.Address)
}
