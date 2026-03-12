// Package cache contains functions for working with caches.
package cache

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Clear clears the directory of all files and directories.
// The directory itself is not removed.
func Clear(dir string) error {
	root, err := os.OpenRoot(dir)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	fs, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}

	for _, f := range fs {
		err = root.RemoveAll(f.Name())
		if err != nil {
			return err
		}
	}

	return nil
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
