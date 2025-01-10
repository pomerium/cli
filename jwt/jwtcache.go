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

	"github.com/pomerium/cli/internal/cache"
	"github.com/pomerium/pomerium/pkg/cryptutil"
)

// predefined cache errors
var (
	ErrExpired  = errors.New("expired")
	ErrInvalid  = errors.New("invalid")
	ErrNotFound = errors.New("not found")
)

// A JWTCache loads and stores JWTs.
type JWTCache interface {
	DeleteJWT(key string) error
	LoadJWT(key string) (rawJWT string, err error)
	StoreJWT(key string, rawJWT string) error
}

var (
	globalJWTCacheMu sync.Mutex
	globalJWTCache   JWTCache
)

// NewJWTCache creates a new JWT Cache. A local cache is used unless there is an error, otherwise an in-memory cache is used.
func NewJWTCache() JWTCache {
	globalJWTCacheMu.Lock()
	defer globalJWTCacheMu.Unlock()

	if globalJWTCache != nil {
		return globalJWTCache
	}

	var err error
	globalJWTCache, err = NewLocalJWTCache()
	if err == nil {
		return globalJWTCache
	}
	globalJWTCache = NewMemoryJWTCache()
	log.Error().Err(err).Msg("error creating local JWT cache, using in-memory JWT cache")
	return globalJWTCache
}

// A LocalJWTCache stores files in the user's cache directory.
type LocalJWTCache struct {
	dir string
}

// NewLocalJWTCache creates a new LocalJWTCache.
func NewLocalJWTCache() (*LocalJWTCache, error) {
	dir, err := cache.JWTsPath()
	if err != nil {
		return nil, err
	}

	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("error creating user cache directory: %w", err)
	}

	return &LocalJWTCache{
		dir: dir,
	}, nil
}

// DeleteJWT deletes a raw JWT from the local cache.
func (cache *LocalJWTCache) DeleteJWT(key string) error {
	path := filepath.Join(cache.dir, cache.fileName(key))
	err := os.Remove(path)
	if os.IsNotExist(err) {
		err = nil
	}
	return err
}

// LoadJWT loads a raw JWT from the local cache.
func (cache *LocalJWTCache) LoadJWT(key string) (rawJWT string, err error) {
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
func (cache *LocalJWTCache) StoreJWT(key string, rawJWT string) error {
	path := filepath.Join(cache.dir, cache.fileName(key))
	err := os.WriteFile(path, []byte(rawJWT), 0o600)
	if err != nil {
		return err
	}

	return nil
}

func (cache *LocalJWTCache) hash(str string) string {
	h := cryptutil.Hash("LocalJWTCache", []byte(str))
	return base36.EncodeBytes(h)
}

func (cache *LocalJWTCache) fileName(key string) string {
	return cache.hash(key) + ".jwt"
}

// A MemoryJWTCache stores JWTs in an in-memory map.
type MemoryJWTCache struct {
	mu      sync.Mutex
	entries map[string]string
}

// NewMemoryJWTCache creates a new in-memory JWT cache.
func NewMemoryJWTCache() *MemoryJWTCache {
	return &MemoryJWTCache{entries: make(map[string]string)}
}

// DeleteJWT deletes a JWT from the in-memory map.
func (cache *MemoryJWTCache) DeleteJWT(key string) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	delete(cache.entries, key)
	return nil
}

// LoadJWT loads a JWT from the in-memory map.
func (cache *MemoryJWTCache) LoadJWT(key string) (rawJWT string, err error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	rawJWT, ok := cache.entries[key]
	if !ok {
		return "", ErrNotFound
	}

	return rawJWT, checkExpiry(rawJWT)
}

// StoreJWT stores a JWT in the in-memory map.
func (cache *MemoryJWTCache) StoreJWT(key string, rawJWT string) error {
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
		Expiry int64 `json:"exp"`
	}
	err = json.Unmarshal(tok.UnsafePayloadWithoutVerification(), &claims)
	if err != nil {
		return ErrInvalid
	}

	expiresAt := time.Unix(claims.Expiry, 0)
	if expiresAt.Before(time.Now()) {
		return ErrExpired
	}

	return nil
}

func GetCacheKey(host string, tlsConfig *tls.Config) string {
	return fmt.Sprintf("%s|%v", host, tlsConfig != nil)
}
