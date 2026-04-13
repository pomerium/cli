package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/pomerium/cli/authclient"
	"github.com/pomerium/cli/internal/httputil"
	"github.com/pomerium/cli/jwt"
)

type authTokenClient interface {
	CheckBearerToken(ctx context.Context, serverURL *url.URL, bearerToken string) error
	GetJWT(ctx context.Context, serverURL *url.URL, onOpenBrowser func(string)) (string, error)
}

type authCommandDeps struct {
	cacheLastURL  func(string)
	getCache      func() jwt.Cache
	getTLSConfig  func() (*tls.Config, error)
	newAuthClient func(*tls.Config) authTokenClient
}

type authCommandRunner struct {
	deps authCommandDeps
}

func init() {
	authCmd := newAuthCommand(authCommandDeps{
		cacheLastURL: cacheLastURL,
		getCache:     jwt.GetCache,
		getTLSConfig: getTLSConfig,
		newAuthClient: func(tlsConfig *tls.Config) authTokenClient {
			return authclient.New(
				authclient.WithBrowserCommand(browserOptions.command),
				authclient.WithServiceAccount(serviceAccountOptions.serviceAccount),
				authclient.WithServiceAccountFile(serviceAccountOptions.serviceAccountFile),
				authclient.WithTLSConfig(tlsConfig),
			)
		},
	})
	rootCmd.AddCommand(authCmd)
}

func newAuthCommand(deps authCommandDeps) *cobra.Command {
	runner := authCommandRunner{deps: deps}

	cmd := &cobra.Command{
		Use:   "auth server-url",
		Short: "print the authorization token for a route",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawJWT, err := runner.run(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), rawJWT)
			return err
		},
	}

	addBrowserFlags(cmd)
	addServiceAccountFlags(cmd)
	addTLSFlags(cmd)

	return cmd
}

func (r authCommandRunner) run(ctx context.Context, rawServerURL string) (string, error) {
	serverURL, err := url.Parse(rawServerURL)
	if err != nil || serverURL.Scheme == "" || serverURL.Host == "" {
		return "", fmt.Errorf("invalid server url: %s", rawServerURL)
	}

	r.deps.cacheLastURL(rawServerURL)

	var tlsConfig *tls.Config
	if serverURL.Scheme == "https" {
		tlsConfig, err = r.deps.getTLSConfig()
		if err != nil {
			return "", err
		}
	}

	cacheKey := jwt.CacheKeyForHost(serverURL.Host, tlsConfig)
	cache := r.deps.getCache()
	authClient := r.deps.newAuthClient(tlsConfig)

	rawJWT, err := cache.LoadJWT(cacheKey)
	switch {
	case err == nil:
		checkErr := authClient.CheckBearerToken(ctx, serverURL, "Pomerium-"+rawJWT)
		switch {
		case checkErr == nil:
			return rawJWT, nil
		case !errors.Is(checkErr, httputil.ErrUnauthenticated):
			return "", fmt.Errorf("error validating cached JWT: %w", checkErr)
		}
	case errors.Is(err, jwt.ErrExpired), errors.Is(err, jwt.ErrInvalid), errors.Is(err, jwt.ErrNotFound):
	default:
		return "", fmt.Errorf("error loading JWT: %w", err)
	}

	rawJWT, err = authClient.GetJWT(ctx, serverURL, func(_ string) {})
	if err != nil {
		return "", fmt.Errorf("error retrieving JWT: %w", err)
	}
	if err := validateRawJWT(rawJWT); err != nil {
		return "", fmt.Errorf("error validating JWT: %w", err)
	}

	if err := cache.StoreJWT(cacheKey, rawJWT); err != nil {
		return "", fmt.Errorf("error storing JWT: %w", err)
	}

	return rawJWT, nil
}

func validateRawJWT(rawJWT string) error {
	// Keep token parsing consistent with the existing CLI auth flows.
	creds, err := parseToken(rawJWT)
	if err != nil {
		return err
	}
	if expiresAt := creds.Status.ExpirationTimestamp; !expiresAt.IsZero() && expiresAt.Before(time.Now()) {
		return jwt.ErrExpired
	}
	return nil
}
