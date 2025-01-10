package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/pomerium/cli/authclient"
)

func init() {
	addBrowserFlags(routesListCmd)
	addServiceAccountFlags(routesListCmd)
	addTLSFlags(routesListCmd)
	routesCmd.AddCommand(routesListCmd)
	rootCmd.AddCommand(routesCmd)
}

var routesCmd = &cobra.Command{
	Use:   "routes",
	Short: "commands for routes",
}

var routesListCmd = &cobra.Command{
	Use:   "list",
	Short: "list routes",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var dst string
		if len(args) == 1 {
			dst = args[0]
		} else {
			return fmt.Errorf("server URL required")
		}

		serverURL, err := url.Parse(dst)
		if err != nil {
			return fmt.Errorf("invalid server URL")
		}

		var tlsConfig *tls.Config
		if serverURL.Scheme == "https" {
			tlsConfig, err = getTLSConfig()
			if err != nil {
				return err
			}
		}

		ctx := context.Background()

		ac := authclient.New(
			authclient.WithBrowserCommand(browserOptions.command),
			authclient.WithServiceAccount(serviceAccountOptions.serviceAccount),
			authclient.WithServiceAccountFile(serviceAccountOptions.serviceAccountFile),
			authclient.WithTLSConfig(tlsConfig),
		)
		rawJWT, err := ac.GetJWT(ctx, serverURL, func(s string) {})
		if err != nil {
			return fmt.Errorf("error retrieving JWT: %w", err)
		}

		routes, err := fetchRoutesPortal(ctx, serverURL, rawJWT)
		if err != nil {
			return err
		}

		for _, route := range routes {
			fmt.Println("Route", route.Name)
			fmt.Println("id:", route.ID)
			fmt.Println("type:", route.Type)
			fmt.Println("from:", route.From)
			fmt.Println("description:", route.Description)
			if route.ConnectCommand != "" {
				fmt.Println("connect_command:", route.ConnectCommand)
			}
			fmt.Println()
		}
		return nil
	},
}

type PortalRoute struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	From           string `json:"from"`
	Description    string `json:"description"`
	ConnectCommand string `json:"connect_command,omitempty"`
	LogoURL        string `json:"logo_url"`
}

func fetchRoutesPortal(ctx context.Context, serverURL *url.URL, rawJWT string) ([]PortalRoute, error) {
	serverURL.Path = "/.pomerium/api/v1/routes"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer Pomerium-"+rawJWT)

	var tlsConfig *tls.Config
	if serverURL.Scheme == "https" {
		tlsConfig, err = getTLSConfig()
		if err != nil {
			return nil, err
		}
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
		Routes []PortalRoute `json:"routes"`
	}
	err = json.NewDecoder(res.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	return data.Routes, nil
}
