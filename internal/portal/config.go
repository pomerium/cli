package portal

import "crypto/tls"

type config struct {
	browserCommand     string
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
	for _, option := range options {
		option(cfg)
	}
	return cfg
}
