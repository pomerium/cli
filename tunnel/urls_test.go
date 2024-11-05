package tunnel

import (
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseURLs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                     string
		destination, pomeriumURL string
		destinationAddr          string
		proxyURL                 string
		err                      error
	}{
		{"invalid destination", "", "", "", "", errors.New("invalid destination")},
		{"host:port", "redis.example.com:6379", "", "redis.example.com:6379", "https://redis.example.com:443", nil},
		{"https url", "tcp+https://redis.example.com:6379", "", "redis.example.com:6379", "https://redis.example.com:443", nil},
		{"http url", "http://redis.example.com:6379", "", "redis.example.com:6379", "http://redis.example.com:80", nil},
		{"https url path", "https://proxy.example.com/redis.example.com:6379", "", "redis.example.com:6379", "https://proxy.example.com:443", nil},
		{"non standard port path", "https://proxy.example.com:8443/redis.example.com:6379", "", "redis.example.com:6379", "https://proxy.example.com:8443", nil},

		{"invalid pomerium url", "redis.example.com:6379", "example.com:1234", "", "", errors.New("invalid pomerium url")},
		{"pomerium url", "redis.example.com:6379", "https://proxy.example.com", "redis.example.com:6379", "https://proxy.example.com:443", nil},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var expectedProxyURL *url.URL
			if tc.proxyURL != "" {
				expectedProxyURL = must(url.Parse(tc.proxyURL))
			}

			destinationAddr, proxyURL, err := ParseURLs(tc.destination, tc.pomeriumURL)
			assert.Equal(t, tc.destinationAddr, destinationAddr)
			assert.Equal(t, expectedProxyURL, proxyURL)
			assert.Equal(t, tc.err, err)
		})
	}
}

func must[T any](ret T, err error) T {
	if err != nil {
		panic(err)
	}
	return ret
}
