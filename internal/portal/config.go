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

type Option func(cfg *config)

func WithBrowserCommand(browserCommand string) Option {
	return func(cfg *config) {
		cfg.browserCommand = browserCommand
	}
}

func WithJWTCache(jwtCache jwt.Cache) Option {
	return func(cfg *config) {
		cfg.jwtCache = jwtCache
	}
}

func WithServiceAccount(serviceAccount string) Option {
	return func(cfg *config) {
		cfg.serviceAccount = serviceAccount
	}
}

func WithServiceAccountFile(serviceAccountFile string) Option {
	return func(cfg *config) {
		cfg.serviceAccountFile = serviceAccountFile
	}
}

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
