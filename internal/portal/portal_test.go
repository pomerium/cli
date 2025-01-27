package portal_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pomerium/cli/internal/portal"
)

func TestPortal(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer Pomerium-SERVICE-ACCOUNT", r.Header.Get("Authorization"))
		assert.Equal(t, "/.pomerium/api/v1/routes", r.URL.Path)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"routes": []portal.Route{
				{ID: "r1", Name: "route-1", Type: "http", From: "https://r1.example.com", Description: "Route #1"},
				{ID: "r2", Name: "route-2", Type: "http", From: "https://r2.example.com", Description: "Route #2"},
				{ID: "r3", Name: "route-3", Type: "http", From: "https://r3.example.com", Description: "Route #3"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	p := portal.New(
		portal.WithServiceAccount("SERVICE-ACCOUNT"),
		portal.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true,
		}),
	)
	routes, err := p.ListRoutes(ctx, srv.URL)
	assert.NoError(t, err)
	assert.Equal(t, []portal.Route{
		{ID: "r1", Name: "route-1", Type: "http", From: "https://r1.example.com", Description: "Route #1"},
		{ID: "r2", Name: "route-2", Type: "http", From: "https://r2.example.com", Description: "Route #2"},
		{ID: "r3", Name: "route-3", Type: "http", From: "https://r3.example.com", Description: "Route #3"},
	}, routes)
}
