package cache_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pomerium/cli/internal/cache"
)

func TestClear(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	rootDir, err := cache.RootPath()
	require.NoError(t, err)

	jwtDir, err := cache.JWTsPath()
	require.NoError(t, err)

	assert.NoError(t, cache.Clear(jwtDir), "should not return an error if the cache directory does not exist")

	require.NoError(t, os.MkdirAll(jwtDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(jwtDir, "example.txt"), []byte("EXAMPLE"), 0o600))

	assert.NoError(t, cache.Clear(rootDir), "should not return an error when clearing")

	fs, err := os.ReadDir(rootDir)
	require.NoError(t, err)
	assert.Empty(t, fs, "should remove all files and directories")
}
