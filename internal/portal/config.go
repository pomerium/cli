package portal

import (
	"crypto/tls"

	"github.com/pomerium/cli/jwt"
)

type config struct {
	browserCommand     string
	jwtCache           jwt.Cache
	serviceAccount     string
	serviceAccountFile string
	tlsConfig          *tls.Config
}

// An Option customizes the portal config.
type Option func(cfg *config)

// WithBrowserCommand sets the browser command in the portal config.
func WithBrowserCommand(browserCommand string) Option {
	return func(cfg *config) {
		cfg.browserCommand = browserCommand
	}
}

// WithJWTCache sets the jwt cache in the portal config.
func WithJWTCache(jwtCache jwt.Cache) Option {
	return func(cfg *config) {
		cfg.jwtCache = jwtCache
	}
}

// WithServiceAccount sets the service account in the portal config.
func WithServiceAccount(serviceAccount string) Option {
	return func(cfg *config) {
		cfg.serviceAccount = serviceAccount
	}
}

// WithServiceAccountFile sets the service account file in the portal config.
func WithServiceAccountFile(serviceAccountFile string) Option {
	return func(cfg *config) {
		cfg.serviceAccountFile = serviceAccountFile
	}
}

// WithTLSConfig sets the tls config in the portal config.
func WithTLSConfig(tlsConfig *tls.Config) Option {
	return func(cfg *config) {
		cfg.tlsConfig = tlsConfig
	}
}

func getConfig(options ...Option) *config {
	cfg := new(config)
	WithJWTCache(jwt.GetCache())(cfg)
	for _, option := range options {
		option(cfg)
	}
	return cfg
}
