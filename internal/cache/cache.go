// Package cache contains functions for working with caches.
package cache

import (
	"os"
	"path/filepath"
)

// Clear clears the cache.
func Clear() error {
	root, err := RootPath()
	if err != nil {
		return err
	}
	return os.RemoveAll(root)
}

// RootPath returns the root cache path.
func RootPath() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "pomerium-cli"), nil
}

// ExecCredentialsPath returns the path to the exec credentials.
func ExecCredentialsPath() (string, error) {
	root, err := RootPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "exec-credential"), nil
}

// JWTsPath returns the path to the jwts.
func JWTsPath() (string, error) {
	root, err := RootPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "jwts"), nil
}

// LastURLPath returns the last URL.
func LastURLPath() (string, error) {
	root, err := RootPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "last-url"), nil
}
