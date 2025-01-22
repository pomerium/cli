package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pomerium/cli/internal/portal"
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
	Use:   "list server-url",
	Short: "list routes",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rawServerURL := ""
		if len(args) > 0 {
			rawServerURL = args[0]
		} else {
			rawServerURL = loadLastURL()
		}
		if rawServerURL == "" {
			return fmt.Errorf("server-url is required")
		}

		cacheLastURL(rawServerURL)

		tlsConfig, err := getTLSConfig()
		if err != nil {
			return err
		}

		p := portal.New(
			portal.WithBrowserCommand(browserOptions.command),
			portal.WithServiceAccount(serviceAccountOptions.serviceAccount),
			portal.WithServiceAccountFile(serviceAccountOptions.serviceAccountFile),
			portal.WithTLSConfig(tlsConfig),
		)
		routes, err := p.ListRoutes(cmd.Context(), rawServerURL)
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
