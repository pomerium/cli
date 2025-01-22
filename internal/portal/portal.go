// Package portal contains functions for listing routes.
package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/pomerium/cli/authclient"
	"github.com/pomerium/cli/internal/httputil"
	"github.com/pomerium/cli/jwt"
	"github.com/pomerium/pomerium/proxy/portal"
)

type Route = portal.Route

type Portal struct {
	cfg        *config
	authClient *authclient.AuthClient
}

// New creates a new Portal.
func New(options ...Option) *Portal {
	p := &Portal{
		cfg: getConfig(options...),
	}
	p.authClient = authclient.New(
		authclient.WithBrowserCommand(p.cfg.browserCommand),
		authclient.WithServiceAccount(p.cfg.serviceAccount),
		authclient.WithServiceAccountFile(p.cfg.serviceAccountFile),
		authclient.WithTLSConfig(p.cfg.tlsConfig),
	)
	return p
}

func (p *Portal) ListRoutes(ctx context.Context, rawServerURL string) ([]Route, error) {
	serverURL, err := url.Parse(rawServerURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing raw server url: %w", err)
	}

	return p.listRoutesWithCachedJWT(ctx, serverURL)
}

func (p *Portal) listRoutesWithCachedJWT(ctx context.Context, serverURL *url.URL) ([]Route, error) {
	cacheKey := jwt.CacheKeyForHost(serverURL.Host, p.cfg.tlsConfig)

	// load the jwt
	rawJWT, err := p.cfg.jwtCache.LoadJWT(cacheKey)
	switch {
	case errors.Is(err, jwt.ErrExpired), errors.Is(err, jwt.ErrInvalid), errors.Is(err, jwt.ErrNotFound):
		// if the jwt isn't valid, get a new jwt and then try listing the routes again
		return p.listRoutesWithNewJWT(ctx, serverURL)
	case err != nil:
		return nil, fmt.Errorf("error loading JWT: %w", err)
	}

	// list the routes using our cached jwt
	routes, err := p.listRoutes(ctx, serverURL, rawJWT)
	if errors.Is(err, httputil.ErrUnauthenticated) {
		// if we aren't authenticated, try to login first before returning an error
		return p.listRoutesWithNewJWT(ctx, serverURL)
	} else if err != nil {
		_ = p.cfg.jwtCache.DeleteJWT(cacheKey)
		return nil, fmt.Errorf("error listing routes: %w", err)
	}

	return routes, nil
}

func (p *Portal) listRoutesWithNewJWT(ctx context.Context, serverURL *url.URL) ([]Route, error) {
	cacheKey := jwt.CacheKeyForHost(serverURL.Host, p.cfg.tlsConfig)

	rawJWT, err := p.authClient.GetJWT(ctx, serverURL, func(s string) {})
	if err != nil {
		return nil, fmt.Errorf("error retrieving JWT: %w", err)
	}
	err = p.cfg.jwtCache.StoreJWT(cacheKey, rawJWT)
	if err != nil {
		return nil, fmt.Errorf("error storing JWT: %w", err)
	}

	routes, err := p.listRoutes(ctx, serverURL, rawJWT)
	if err != nil {
		_ = p.cfg.jwtCache.DeleteJWT(cacheKey)
		return nil, fmt.Errorf("error listing routes: %w", err)
	}
	return routes, err
}

func (p *Portal) listRoutes(ctx context.Context, serverURL *url.URL, rawJWT string) ([]Route, error) {
	serverURL = serverURL.ResolveReference(&url.URL{
		Path: "/.pomerium/api/v1/routes",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating routes portal request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer Pomerium-"+rawJWT)

	bs, err := httputil.Fetch(ctx, p.cfg.tlsConfig, req)
	if err != nil {
		return nil, fmt.Errorf("error fetching routes portal: %w", err)
	}

	var res struct {
		Routes []portal.Route `json:"routes"`
	}
	err = json.Unmarshal(bs, &res)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling routes portal response: %w", err)
	}

	return res.Routes, nil
}
