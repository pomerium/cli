package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pomerium/cli/internal/httputil"
	"github.com/pomerium/cli/jwt"
)

type fakeAuthTokenClient struct {
	checkErr    error
	getJWT      string
	getJWTErr   error
	checkCalls  []string
	getJWTCalls []string
}

func (f *fakeAuthTokenClient) CheckBearerToken(_ context.Context, serverURL *url.URL, bearerToken string) error {
	f.checkCalls = append(f.checkCalls, serverURL.String()+"|"+bearerToken)
	return f.checkErr
}

func (f *fakeAuthTokenClient) GetJWT(_ context.Context, serverURL *url.URL, _ func(string)) (string, error) {
	f.getJWTCalls = append(f.getJWTCalls, serverURL.String())
	if f.getJWTErr != nil {
		return "", f.getJWTErr
	}
	return f.getJWT, nil
}

func TestAuthCommandRunner(t *testing.T) {
	t.Parallel()

	t.Run("cache hit", func(t *testing.T) {
		t.Parallel()

		cache := jwt.NewMemoryCache()
		rawJWT := mustRawJWT(t, time.Now().Add(time.Hour))
		serverURL := "https://route.example.com"
		cacheKey := jwt.CacheKeyForHost("route.example.com", &tls.Config{})
		require.NoError(t, cache.StoreJWT(cacheKey, rawJWT))

		authClient := &fakeAuthTokenClient{}
		var cachedURL string
		runner := authCommandRunner{
			deps: authCommandDeps{
				cacheLastURL: func(raw string) { cachedURL = raw },
				getCache:     func() jwt.Cache { return cache },
				getTLSConfig: func() (*tls.Config, error) { return &tls.Config{}, nil },
				newAuthClient: func(*tls.Config) authTokenClient {
					return authClient
				},
			},
		}

		got, err := runner.run(context.Background(), serverURL)
		require.NoError(t, err)
		assert.Equal(t, rawJWT, got)
		assert.Equal(t, serverURL, cachedURL)
		assert.Equal(t, []string{serverURL + "|Pomerium-" + rawJWT}, authClient.checkCalls)
		assert.Empty(t, authClient.getJWTCalls)
	})

	t.Run("cache hit with failed live check refreshes token", func(t *testing.T) {
		t.Parallel()

		cache := jwt.NewMemoryCache()
		cachedJWT := mustRawJWT(t, time.Now().Add(time.Hour))
		refreshedJWT := mustRawJWT(t, time.Now().Add(2*time.Hour))
		serverURL := "https://route.example.com"
		cacheKey := jwt.CacheKeyForHost("route.example.com", &tls.Config{})
		require.NoError(t, cache.StoreJWT(cacheKey, cachedJWT))

		authClient := &fakeAuthTokenClient{
			checkErr: httputil.ErrUnauthenticated,
			getJWT:   refreshedJWT,
		}
		runner := authCommandRunner{
			deps: authCommandDeps{
				cacheLastURL: func(string) {},
				getCache:     func() jwt.Cache { return cache },
				getTLSConfig: func() (*tls.Config, error) { return &tls.Config{}, nil },
				newAuthClient: func(*tls.Config) authTokenClient {
					return authClient
				},
			},
		}

		got, err := runner.run(context.Background(), serverURL)
		require.NoError(t, err)
		assert.Equal(t, refreshedJWT, got)
		assert.Equal(t, []string{serverURL + "|Pomerium-" + cachedJWT}, authClient.checkCalls)
		assert.Equal(t, []string{serverURL}, authClient.getJWTCalls)

		stored, err := cache.LoadJWT(cacheKey)
		require.NoError(t, err)
		assert.Equal(t, refreshedJWT, stored)
	})

	t.Run("cache hit with non-auth validation error returns error", func(t *testing.T) {
		t.Parallel()

		cache := jwt.NewMemoryCache()
		cachedJWT := mustRawJWT(t, time.Now().Add(time.Hour))
		serverURL := "https://route.example.com"
		cacheKey := jwt.CacheKeyForHost("route.example.com", &tls.Config{})
		require.NoError(t, cache.StoreJWT(cacheKey, cachedJWT))

		authClient := &fakeAuthTokenClient{checkErr: errors.New("dial tcp: lookup failed")}
		runner := authCommandRunner{
			deps: authCommandDeps{
				cacheLastURL: func(string) {},
				getCache:     func() jwt.Cache { return cache },
				getTLSConfig: func() (*tls.Config, error) { return &tls.Config{}, nil },
				newAuthClient: func(*tls.Config) authTokenClient {
					return authClient
				},
			},
		}

		_, err := runner.run(context.Background(), serverURL)
		require.Error(t, err)
		assert.EqualError(t, err, "error validating cached JWT: dial tcp: lookup failed")
		assert.Equal(t, []string{serverURL + "|Pomerium-" + cachedJWT}, authClient.checkCalls)
		assert.Empty(t, authClient.getJWTCalls)
	})

	t.Run("cache miss", func(t *testing.T) {
		t.Parallel()

		cache := jwt.NewMemoryCache()
		refreshedJWT := mustRawJWT(t, time.Now().Add(time.Hour))
		serverURL := "http://route.example.com"
		cacheKey := jwt.CacheKeyForHost("route.example.com", nil)

		authClient := &fakeAuthTokenClient{getJWT: refreshedJWT}
		runner := authCommandRunner{
			deps: authCommandDeps{
				cacheLastURL: func(string) {},
				getCache:     func() jwt.Cache { return cache },
				getTLSConfig: func() (*tls.Config, error) { return nil, errors.New("should not be called") },
				newAuthClient: func(*tls.Config) authTokenClient {
					return authClient
				},
			},
		}

		got, err := runner.run(context.Background(), serverURL)
		require.NoError(t, err)
		assert.Equal(t, refreshedJWT, got)
		assert.Empty(t, authClient.checkCalls)
		assert.Equal(t, []string{serverURL}, authClient.getJWTCalls)

		stored, err := cache.LoadJWT(cacheKey)
		require.NoError(t, err)
		assert.Equal(t, refreshedJWT, stored)
	})

	t.Run("https tls config error", func(t *testing.T) {
		t.Parallel()

		runner := authCommandRunner{
			deps: authCommandDeps{
				cacheLastURL: func(string) {},
				getCache:     func() jwt.Cache { return jwt.NewMemoryCache() },
				getTLSConfig: func() (*tls.Config, error) { return nil, errors.New("bad tls config") },
				newAuthClient: func(*tls.Config) authTokenClient {
					return &fakeAuthTokenClient{}
				},
			},
		}

		_, err := runner.run(context.Background(), "https://route.example.com")
		require.Error(t, err)
		assert.EqualError(t, err, "bad tls config")
	})

	t.Run("invalid fresh token returns error", func(t *testing.T) {
		t.Parallel()

		cache := jwt.NewMemoryCache()
		authClient := &fakeAuthTokenClient{getJWT: "invalid"}
		runner := authCommandRunner{
			deps: authCommandDeps{
				cacheLastURL: func(string) {},
				getCache:     func() jwt.Cache { return cache },
				getTLSConfig: func() (*tls.Config, error) { return nil, nil },
				newAuthClient: func(*tls.Config) authTokenClient {
					return authClient
				},
			},
		}

		_, err := runner.run(context.Background(), "http://route.example.com")
		require.Error(t, err)
		assert.ErrorContains(t, err, "error validating JWT:")
		_, err = cache.LoadJWT(jwt.CacheKeyForHost("route.example.com", nil))
		assert.ErrorIs(t, err, jwt.ErrNotFound)
	})
}

func TestAuthCommand(t *testing.T) {
	t.Parallel()

	t.Run("prints token", func(t *testing.T) {
		t.Parallel()

		authClient := &fakeAuthTokenClient{getJWT: mustRawJWT(t, time.Now().Add(time.Hour))}
		cmd := newAuthCommand(authCommandDeps{
			cacheLastURL: func(string) {},
			getCache:     func() jwt.Cache { return jwt.NewMemoryCache() },
			getTLSConfig: func() (*tls.Config, error) { return &tls.Config{}, nil },
			newAuthClient: func(*tls.Config) authTokenClient {
				return authClient
			},
		})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"https://route.example.com"})

		require.NoError(t, cmd.ExecuteContext(context.Background()))
		assert.Equal(t, authClient.getJWT+"\n", out.String())
	})

	t.Run("invalid url", func(t *testing.T) {
		t.Parallel()

		cmd := newAuthCommand(authCommandDeps{
			cacheLastURL: func(string) {},
			getCache:     func() jwt.Cache { return jwt.NewMemoryCache() },
			getTLSConfig: func() (*tls.Config, error) { return &tls.Config{}, nil },
			newAuthClient: func(*tls.Config) authTokenClient {
				return &fakeAuthTokenClient{}
			},
		})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"not-a-url"})

		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
		assert.EqualError(t, err, "invalid server url: not-a-url")
	})

	t.Run("requires exact arg", func(t *testing.T) {
		t.Parallel()

		cmd := newAuthCommand(authCommandDeps{
			cacheLastURL: func(string) {},
			getCache:     func() jwt.Cache { return jwt.NewMemoryCache() },
			getTLSConfig: func() (*tls.Config, error) { return &tls.Config{}, nil },
			newAuthClient: func(*tls.Config) authTokenClient {
				return &fakeAuthTokenClient{}
			},
		})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
		assert.EqualError(t, err, "accepts 1 arg(s), received 0")
	})
}

func mustRawJWT(t *testing.T, expiry time.Time) string {
	t.Helper()

	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.HS256,
		Key:       []byte("secret"),
	}, nil)
	require.NoError(t, err)

	payload, err := json.Marshal(map[string]int64{
		"exp": expiry.Unix(),
	})
	require.NoError(t, err)

	object, err := signer.Sign(payload)
	require.NoError(t, err)

	rawJWT, err := object.CompactSerialize()
	require.NoError(t, err)

	return rawJWT
}
