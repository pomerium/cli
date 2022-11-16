package tcptunnel

import (
	"crypto/tls"
	"log"

	"github.com/pomerium/cli/jwt"
)

type config struct {
	jwtCache           jwt.JWTCache
	dstHost            string
	proxyHost          string
	serviceAccount     string
	serviceAccountFile string
	tlsConfig          *tls.Config
	browserConfig      string
}

func getConfig(options ...Option) *config {
	cfg := new(config)
	if jwtCache, err := jwt.NewLocalJWTCache(); err == nil {
		WithJWTCache(jwtCache)(cfg)
	} else {
		log.Printf("error creating local JWT cache, using in-memory JWT cache: %v\n", err)
		WithJWTCache(jwt.NewMemoryJWTCache())(cfg)
	}
	for _, o := range options {
		o(cfg)
	}
	return cfg
}

// An Option modifies the config.
type Option func(*config)

// WithBrowserCommand returns an option to configure the browser command.
func WithBrowserCommand(browserCommand string) Option {
	return func(cfg *config) {
		cfg.browserConfig = browserCommand
	}
}

// WithDestinationHost returns an option to configure the destination host.
func WithDestinationHost(dstHost string) Option {
	return func(cfg *config) {
		cfg.dstHost = dstHost
	}
}

// WithJWTCache returns an option to configure the jwt cache.
func WithJWTCache(jwtCache jwt.JWTCache) Option {
	return func(cfg *config) {
		cfg.jwtCache = jwtCache
	}
}

// WithProxyHost returns an option to configure the proxy host.
func WithProxyHost(proxyHost string) Option {
	return func(cfg *config) {
		cfg.proxyHost = proxyHost
	}
}

// WithServiceAccount sets the service account in the config.
func WithServiceAccount(serviceAccount string) Option {
	return func(cfg *config) {
		cfg.serviceAccount = serviceAccount
	}
}

// WithServiceAccountFile sets the service account file in the config.
func WithServiceAccountFile(file string) Option {
	return func(cfg *config) {
		cfg.serviceAccountFile = file
	}
}

// WithTLSConfig returns an option to configure the tls config.
func WithTLSConfig(tlsConfig *tls.Config) Option {
	return func(cfg *config) {
		if tlsConfig != nil {
			tlsConfig = tlsConfig.Clone()
			tlsConfig.NextProtos = []string{"http/1.1"} // disable http/2 in ALPN
		}
		cfg.tlsConfig = tlsConfig
	}
}
