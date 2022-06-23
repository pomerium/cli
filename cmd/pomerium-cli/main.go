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
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
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
