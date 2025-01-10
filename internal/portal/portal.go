package portal

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/pomerium/cli/proto"
)

// A Portal is used to fetch routes in pomerium.
type Portal struct {
	cfg *config
}

// New creates a new Portal.
func New(options ...Option) *Portal {
	return &Portal{
		cfg: getConfig(options...),
	}
}

func (p *Portal) ListRoutes(ctx context.Context, rawServerURL string) ([]*proto.PortalRoute, error) {
	serverURL, err := url.Parse(rawServerURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing raw server url: %w", err)
	}

	serverURL.Path = "/.pomerium/api/v1/routes"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer Pomerium-"+rawJWT)

	var tlsConfig *tls.Config
	if serverURL.Scheme == "https" {
		tlsConfig = p.cfg.tlsConfig
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	hc := &http.Client{
		Transport: transport,
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get url: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("unexpected status code: %s", res.Status)
	}

	var data struct {
		Routes []*proto.PortalRoute `json:"routes"`
	}
	err = json.NewDecoder(res.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	return data.Routes, nil
}
