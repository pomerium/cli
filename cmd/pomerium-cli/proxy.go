package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/elazarl/goproxy"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/pomerium/cli/tcptunnel"
)

var proxyCmdOptions struct {
	listen       string
	pomeriumURL  string
	proxyDomains []string
}

func init() {
	addServiceAccountFlags(proxyCmd)
	addTLSFlags(proxyCmd)
	flags := proxyCmd.Flags()
	flags.StringVar(&proxyCmdOptions.listen, "listen", "127.0.0.1:3128",
		"local address to start a listener on")
	flags.StringVar(&proxyCmdOptions.pomeriumURL, "pomerium-url", "",
		"the URL of the pomerium server to connect to")
	flags.StringArrayVar(&proxyCmdOptions.proxyDomains, "proxy-domain", []string{},
		"connections to this domain will be proxied")
	rootCmd.AddCommand(proxyCmd)
}

var proxyCmd = &cobra.Command{
	Use:    "proxy",
	Short:  "creates a https proxy that proxies certain domains via a TCP tunnel through Pomerium",
	Args:   cobra.ExactArgs(0),
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		proxy := goproxy.NewProxyHttpServer()

		domainRegexes, err := makeDomainRegexes()
		if err != nil {
			return err
		}

		// HTTPS proxy calls matching domainRegex
		for _, domainRegex := range domainRegexes {
			proxy.OnRequest(goproxy.ReqHostMatches(domainRegex)).HijackConnect(hijackProxyConnect)
		}

		// HTTP
		// Do nothing, just transparantly proxy

		srv := &http.Server{
			Addr:    proxyCmdOptions.listen,
			Handler: proxy,
		}

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-c
			cancel()
			err := srv.Shutdown(ctx)
			if err != nil {
				log.Error().Err(err).Msg("Proxy listener did not shutdown gracefully")
			}
		}()

		log.Info().Msgf("Proxy running at %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("proxy failed to start: %v", err)
		}
		return nil
	},
}

func makeDomainRegexes() ([]*regexp.Regexp, error) {
	if len(proxyCmdOptions.proxyDomains) == 0 {
		return nil, fmt.Errorf("--proxy-domain is required")
	}
	var domainRegexes []*regexp.Regexp
	for _, proxyDomain := range proxyCmdOptions.proxyDomains {
		domainRegex, err := regexp.Compile(fmt.Sprintf(`^.*%s(:\d+)?$`, regexp.QuoteMeta(proxyDomain)))
		if err != nil {
			return nil, fmt.Errorf("invalid proxy-domain")
		}
		domainRegexes = append(domainRegexes, domainRegex)
	}
	return domainRegexes, nil
}

func newTCPTunnel(dstHost string, specificPomeriumURL string) (*tcptunnel.Tunnel, error) {
	dstHostname, dstPort, err := net.SplitHostPort(dstHost)
	if err != nil {
		return nil, fmt.Errorf("invalid destination: %w", err)
	}

	// This is a workaround for issues (probably?) caused by envoy doing strange things if the frontend port is 443.
	// Rewrite port 443 to port 8000.
	if dstPort == "443" {
		dstPort = "8000"
	}

	pomeriumURL := &url.URL{
		Scheme: "https",
		Host:   net.JoinHostPort(dstHostname, "443"),
	}
	if specificPomeriumURL != "" {
		pomeriumURL, err = url.Parse(specificPomeriumURL)
		if err != nil {
			return nil, fmt.Errorf("invalid pomerium URL: %w", err)
		}
		if !strings.Contains(pomeriumURL.Host, ":") {
			if pomeriumURL.Scheme == "https" {
				pomeriumURL.Host = net.JoinHostPort(pomeriumURL.Hostname(), "443")
			} else {
				pomeriumURL.Host = net.JoinHostPort(pomeriumURL.Hostname(), "80")
			}
		}
	}

	var tlsConfig *tls.Config
	if pomeriumURL.Scheme == "https" {
		tlsConfig, err = getTLSConfig()
		if err != nil {
			return nil, fmt.Errorf("invalid destination: %w", err)
		}
	}

	return tcptunnel.New(
		tcptunnel.WithDestinationHost(net.JoinHostPort(dstHostname, dstPort)),
		tcptunnel.WithProxyHost(pomeriumURL.Host),
		tcptunnel.WithServiceAccount(serviceAccountOptions.serviceAccount),
		tcptunnel.WithServiceAccountFile(serviceAccountOptions.serviceAccountFile),
		tcptunnel.WithTLSConfig(tlsConfig),
	), nil
}

func hijackProxyConnect(req *http.Request, client net.Conn, ctx *goproxy.ProxyCtx) {
	dst := req.RequestURI
	defer client.Close()
	tun, err := newTCPTunnel(dst, proxyCmdOptions.pomeriumURL)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create TCP tunnel")
		_, err = client.Write([]byte("HTTP/1.1 500 Cannot reach destination\r\n\r\n"))
		if err != nil {
			log.Error().Err(err).Msg("Failed to send error response to client")
		}
		return
	}

	_, err = client.Write([]byte("HTTP/1.1 200 Connection established\n\n"))
	if err != nil {
		log.Error().Err(err).Msg("Failed to send response to client")
		return
	}
	if err := tun.Run(req.Context(), client, tcptunnel.DiscardEvents()); err != nil {
		log.Error().Err(err).Msg("Failed to run TCP tunnel")
	}
}
