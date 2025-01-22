package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/pomerium/cli/tunnel"
)

var udpCmdOptions struct {
	listen      string
	pomeriumURL string
}

var udpCmd = &cobra.Command{
	Use:   "udp destination",
	Short: "creates a UDP tunnel through Pomerium",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		destinationAddr, proxyURL, err := tunnel.ParseURLs(args[0], tcpCmdOptions.pomeriumURL)
		if err != nil {
			return err
		}
		cacheLastURL(proxyURL.String())

		var tlsConfig *tls.Config
		if proxyURL.Scheme == "https" {
			tlsConfig, err = getTLSConfig()
			if err != nil {
				return err
			}
		}

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-c
			cancel()
		}()

		tun := tunnel.New(
			tunnel.WithBrowserCommand(browserOptions.command),
			tunnel.WithDestinationHost(destinationAddr),
			tunnel.WithProxyHost(proxyURL.Host),
			tunnel.WithServiceAccount(serviceAccountOptions.serviceAccount),
			tunnel.WithServiceAccountFile(serviceAccountOptions.serviceAccountFile),
			tunnel.WithTLSConfig(tlsConfig),
		)

		if udpCmdOptions.listen == "-" {
			err = fmt.Errorf("stdout not implemented for UDP")
		} else {
			err = tun.RunUDPListener(ctx, udpCmdOptions.listen)
		}
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "%s\n", err.Error())
			os.Exit(1)
		}

		return nil
	},
}

func init() {
	addBrowserFlags(udpCmd)
	addServiceAccountFlags(udpCmd)
	addTLSFlags(udpCmd)
	flags := udpCmd.Flags()
	flags.StringVar(&udpCmdOptions.listen, "listen", "127.0.0.1:0",
		"local address to start a listener on")
	flags.StringVar(&udpCmdOptions.pomeriumURL, "pomerium-url", "",
		"the URL of the pomerium server to connect to")
	rootCmd.AddCommand(udpCmd)
}
