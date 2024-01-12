package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "commands for working with the cache",
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "clear the cache",
	RunE: func(_ *cobra.Command, _ []string) error {
		root, err := cachePath()
		if err != nil {
			return err
		}
		return os.RemoveAll(root)
	},
}

var cacheLocationCmd = &cobra.Command{
	Use:   "location",
	Short: "print the cache location",
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := cachePath()
		if err != nil {
			return err
		}
		fmt.Println(root)
		return nil
	},
}

func init() {
	cacheCmd.AddCommand(cacheClearCmd)
	cacheCmd.AddCommand(cacheLocationCmd)
	rootCmd.AddCommand(cacheCmd)
}

func cachePath() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "pomerium-cli"), nil
}

func cachedCredentialsPath() (string, error) {
	root, err := cachePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "exec-credential"), nil
}

func cachedCredentialPath(serverURL string) (string, error) {
	h := sha256.New()
	_, _ = h.Write([]byte(serverURL))
	id := hex.EncodeToString(h.Sum(nil))
	p, err := cachedCredentialsPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, id+".json"), nil
}

func clearAllCachedCredentials() error {
	cache, err := cachedCredentialsPath()
	if err != nil {
		return err
	}
	return filepath.Walk(cache, func(p string, fi fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if fi.IsDir() {
			return nil
		}

		return os.Remove(p)
	})
}

func clearCachedCredential(serverURL string) error {
	fn, err := cachedCredentialPath(serverURL)
	if err != nil {
		return err
	}
	return os.Remove(fn)
}

func loadCachedCredential(serverURL string) (*ExecCredential, error) {
	fn, err := cachedCredentialPath(serverURL)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var creds ExecCredential
	err = json.NewDecoder(f).Decode(&creds)
	if err != nil {
		_ = os.Remove(fn)
		return nil, err
	}

	if creds.Status == nil {
		_ = os.Remove(fn)
		return nil, errors.New("creds.status == nil")
	}

	ts := creds.Status.ExpirationTimestamp
	if ts.IsZero() || ts.Before(time.Now()) {
		_ = os.Remove(fn)
		return nil, errors.New("expired")
	}

	return &creds, nil
}

func saveCachedCredential(serverURL string, creds *ExecCredential) error {
	fn, err := cachedCredentialPath(serverURL)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(fn), 0o755)
	if err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	f, err := os.Create(fn)
	if err != nil {
		return fmt.Errorf("failed to create cache file: %w", err)
	}

	err = json.NewEncoder(f).Encode(creds)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to encode credentials to cache file: %w", err)
	}

	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close cache file: %w", err)
	}

	return nil
}
