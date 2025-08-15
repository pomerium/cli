package jwt

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v3"
	"github.com/martinlindhe/base36"
	"github.com/rs/zerolog/log"
	"github.com/volatiletech/null/v9"

	"github.com/pomerium/cli/internal/cache"
	"github.com/pomerium/pomerium/pkg/cryptutil"
)

// predefined cache errors
var (
	ErrExpired  = errors.New("expired")
	ErrInvalid  = errors.New("invalid")
	ErrNotFound = errors.New("not found")
)

// A Cache loads and stores JWTs.
type Cache interface {
	DeleteJWT(key string) error
	LoadJWT(key string) (rawJWT string, err error)
	StoreJWT(key string, rawJWT string) error
}

var (
	globalCacheOnce sync.Once
	globalCache     Cache
)

// GetCache gets the Cache. Either a local one is used or if that's not possible an in-memory one is used.
func GetCache() Cache {
	globalCacheOnce.Do(func() {
		if c, err := NewLocalCache(); err == nil {
			globalCache = c
		} else {
			log.Error().Err(err).Msg("error creating local JWT cache, using in-memory JWT cache")
			globalCache = NewMemoryCache()
		}
	})
	return globalCache
}

// A LocalCache stores files in the user's cache directory.
type LocalCache struct {
	dir string
}

// NewLocalCache creates a new LocalCache.
func NewLocalCache() (*LocalCache, error) {
	dir, err := cache.JWTsPath()
	if err != nil {
		return nil, err
	}

	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("error creating user cache directory: %w", err)
	}

	return &LocalCache{
		dir: dir,
	}, nil
}

// DeleteJWT deletes a raw JWT from the local cache.
func (cache *LocalCache) DeleteJWT(key string) error {
	path := filepath.Join(cache.dir, cache.fileName(key))
	err := os.Remove(path)
	if os.IsNotExist(err) {
		err = nil
	}
	return err
}

// LoadJWT loads a raw JWT from the local cache.
func (cache *LocalCache) LoadJWT(key string) (rawJWT string, err error) {
	path := filepath.Join(cache.dir, cache.fileName(key))
	rawBS, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", ErrNotFound
	} else if err != nil {
		return "", err
	}
	rawJWT = string(rawBS)

	return rawJWT, checkExpiry(rawJWT)
}

// StoreJWT stores a raw JWT in the local cache.
func (cache *LocalCache) StoreJWT(key string, rawJWT string) error {
	err := os.MkdirAll(cache.dir, 0o755)
	if err != nil {
		return err
	}

	path := filepath.Join(cache.dir, cache.fileName(key))
	err = os.WriteFile(path, []byte(rawJWT), 0o600)
	if err != nil {
		return err
	}

	return nil
}

func (cache *LocalCache) hash(str string) string {
	h := cryptutil.Hash("LocalJWTCache", []byte(str))
	return base36.EncodeBytes(h)
}

func (cache *LocalCache) fileName(key string) string {
	return cache.hash(key) + ".jwt"
}

// A MemoryCache stores JWTs in an in-memory map.
type MemoryCache struct {
	mu      sync.Mutex
	entries map[string]string
}

// NewMemoryCache creates a new in-memory JWT cache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{entries: make(map[string]string)}
}

// DeleteJWT deletes a JWT from the in-memory map.
func (cache *MemoryCache) DeleteJWT(key string) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	delete(cache.entries, key)
	return nil
}

// LoadJWT loads a JWT from the in-memory map.
func (cache *MemoryCache) LoadJWT(key string) (rawJWT string, err error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	rawJWT, ok := cache.entries[key]
	if !ok {
		return "", ErrNotFound
	}

	return rawJWT, checkExpiry(rawJWT)
}

// StoreJWT stores a JWT in the in-memory map.
func (cache *MemoryCache) StoreJWT(key string, rawJWT string) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.entries[key] = rawJWT

	return nil
}

func checkExpiry(rawJWT string) error {
	tok, err := jose.ParseSigned(rawJWT)
	if err != nil {
		return ErrInvalid
	}

	var claims struct {
		Expiry null.Int64 `json:"exp"`
	}
	err = json.Unmarshal(tok.UnsafePayloadWithoutVerification(), &claims)
	if err != nil {
		return ErrInvalid
	}

	if claims.Expiry.Valid {
		expiresAt := time.Unix(claims.Expiry.Int64, 0)
		if expiresAt.Before(time.Now()) {
			return ErrExpired
		}
	}

	return nil
}

// CacheKeyForHost returns the cache key for the given host and tls config.
func CacheKeyForHost(host string, tlsConfig *tls.Config) string {
	return fmt.Sprintf("%s|%v", host, tlsConfig != nil)
}
