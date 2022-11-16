package authclient

import (
	"crypto/tls"

	"github.com/skratchdot/open-golang/open"
)

type config struct {
	open               func(rawURL string) error
	serviceAccount     string
	serviceAccountFile string
	tlsConfig          *tls.Config
}

func getConfig(options ...Option) *config {
	cfg := new(config)
	WithBrowserCommand("")(cfg)
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
		if browserCommand == "" {
			cfg.open = open.Run
		} else {
			cfg.open = func(rawURL string) error {
				return open.RunWith(rawURL, browserCommand)
			}
		}
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
		cfg.tlsConfig = tlsConfig.Clone()
	}
}
