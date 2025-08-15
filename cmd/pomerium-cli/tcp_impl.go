package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/pomerium/cli/tunnel"
	"github.com/spf13/pflag"
)

var tcpCmdOptions struct {
	listen      string
	pomeriumURL string
}

func addTcpFlags(flags *pflag.FlagSet) {
	addBrowserFlags(tcpCmd)
	addServiceAccountFlags(tcpCmd)
	addTLSFlags(tcpCmd)
	flags.StringVar(&tcpCmdOptions.listen, "listen", "127.0.0.1:0",
		"local address to start a listener on")
	flags.StringVar(&tcpCmdOptions.pomeriumURL, "pomerium-url", "",
		"the URL of the pomerium server to connect to")
	rootCmd.AddCommand(tcpCmd)
}

func runTcpForever(destination string) error {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-c
		cancel()
	}()

	return runTcp(ctx, destination)
}

func runTcp(ctx context.Context, destination string) error {
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

	if tcpCmdOptions.listen == "-" {
		err = tun.Run(ctx, readWriter{Reader: os.Stdin, Writer: os.Stdout}, tunnel.LogEvents())
	} else {
		err = tun.RunListener(ctx, tcpCmdOptions.listen)
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}

	return nil
}

type readWriter struct {
	io.Reader
	io.Writer
}
