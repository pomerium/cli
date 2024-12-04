package testutil

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

// GetPort gets a free port.
func GetPort(t *testing.T) string {
	t.Helper()

	li, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	_, port, err := net.SplitHostPort(li.Addr().String())
	require.NoError(t, err)

	_ = li.Close()

	return port
}
