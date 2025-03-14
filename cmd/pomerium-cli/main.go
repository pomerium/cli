// Package main implements the pomerium-cli.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/pomerium/cli/certstore"
	"github.com/pomerium/cli/version"
	"github.com/pomerium/pomerium/pkg/cryptutil"
)

var rootCmd = &cobra.Command{
	Use:     "pomerium-cli",
	Version: version.FullVersion(),
}

func main() {
	setupLogger()

	err := rootCmd.ExecuteContext(signalContext())
	if err != nil {
		log.Error().Err(err).Msg("exit")
	}
}

func signalContext() context.Context {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sig := <-sigs
		log.Error().Str("signal", sig.String()).Msg("caught signal, quitting...")
		cancel()
		time.Sleep(time.Second * 2)
		log.Error().Msg("did not shut down gracefully, exit")
		os.Exit(1)
	}()
	return ctx
}

func setupLogger() {
	log.Logger = log.Level(zerolog.InfoLevel)

	// set the log level
	if raw := os.Getenv("LOG_LEVEL"); raw != "" {
		if lvl, err := zerolog.ParseLevel(raw); err == nil {
			log.Logger = log.Logger.Level(lvl)
		}
	}

	zerolog.DefaultContextLogger = &log.Logger
}

func fatalf(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(1)
}

var tlsOptions struct {
	disableTLSVerification bool
	alternateCAPath        string
	caCert                 string
	clientCertPath         string
	clientKeyPath          string
	clientCertFromStore    bool
	clientCertIssuer       string
	clientCertSubject      string
}

func addTLSFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.BoolVar(&tlsOptions.disableTLSVerification, "disable-tls-verification", false,
		"disables TLS verification")
	flags.StringVar(&tlsOptions.alternateCAPath, "alternate-ca-path", "",
		"path to CA certificate to use for HTTP requests")
	flags.StringVar(&tlsOptions.caCert, "ca-cert", "",
		"base64-encoded CA TLS certificate to use for HTTP requests")
	flags.StringVar(&tlsOptions.clientCertPath, "client-cert", "",
		"(optional) PEM-encoded client certificate")
	flags.StringVar(&tlsOptions.clientKeyPath, "client-key", "",
		"(optional) PEM-encoded client certificate")
	if certstore.IsCertstoreSupported {
		flags.BoolVar(&tlsOptions.clientCertFromStore, "client-cert-from-store", false,
			"load client certificate and key from the system trust store [macOS and Windows only]")
		flags.StringVar(&tlsOptions.clientCertIssuer, "client-cert-issuer", "",
			"search system trust store by some attribute of the cert Issuer name "+
				`(e.g. "CN=my trusted CA name")`)
		flags.StringVar(&tlsOptions.clientCertSubject, "client-cert-subject", "",
			"search system trust store by some attribute of the cert Subject name "+
				`(e.g. "O=my organization name")`)
	}
}

func getTLSConfig() (*tls.Config, error) {
	cfg := new(tls.Config)
	if tlsOptions.disableTLSVerification {
		cfg.InsecureSkipVerify = true
	}
	if tlsOptions.caCert != "" || tlsOptions.alternateCAPath != "" {
		var err error
		cfg.RootCAs, err = cryptutil.GetCertPool(tlsOptions.caCert, tlsOptions.alternateCAPath)
		if err != nil {
			return nil, fmt.Errorf("get CA cert: %w", err)
		}
	}
	if tlsOptions.clientCertPath != "" || tlsOptions.clientKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(tlsOptions.clientCertPath, tlsOptions.clientKeyPath)
		if err != nil {
			return nil, fmt.Errorf("loading client cert: %w", err)
		}
		cfg.Certificates = append(cfg.Certificates, cert)
	}
	if tlsOptions.clientCertFromStore {
		f, err := certstore.GetClientCertificateFunc(
			tlsOptions.clientCertIssuer, tlsOptions.clientCertSubject)
		if err != nil {
			return nil, err
		}
		cfg.GetClientCertificate = f
	}
	return cfg, nil
}

var browserOptions struct {
	command string
}

func addBrowserFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&browserOptions.command, "browser-cmd", "",
		"custom browser command to run when opening a URL")
}

var serviceAccountOptions struct {
	serviceAccount     string
	serviceAccountFile string
}

func addServiceAccountFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&serviceAccountOptions.serviceAccount, "service-account", "",
		"the service account JWT to use for authentication")
	flags.StringVar(&serviceAccountOptions.serviceAccountFile, "service-account-file", "",
		"a file containing the service account JWT to use for authentication")
}
