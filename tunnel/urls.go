package tunnel

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func ParseURLs(destination string, pomeriumURL string) (destinationAddr string, proxyURL *url.URL, err error) {
	if strings.Contains(destination, "://") {
		destinationURL, err := url.Parse(destination)
		if err != nil {
			return "", nil, fmt.Errorf("invalid destination")
		}

		paths := strings.Split(destinationURL.Path, "/")[1:]
		if len(paths) == 0 {
			destinationAddr = destinationURL.Host
			proxyURL = &url.URL{
				Scheme: strings.TrimPrefix(strings.TrimPrefix(destinationURL.Scheme, "tcp+"), "udp+"),
				Host:   destinationURL.Hostname(),
			}
		} else {
			destinationAddr = paths[0]
			proxyURL = &url.URL{
				Scheme: strings.TrimPrefix(strings.TrimPrefix(destinationURL.Scheme, "tcp+"), "udp+"),
				Host:   destinationURL.Host,
			}
		}
	} else if h, p, err := net.SplitHostPort(destination); err == nil {
		destinationAddr = net.JoinHostPort(h, p)
		proxyURL = &url.URL{
			Scheme: "https",
			Host:   h,
		}
	} else {
		return "", nil, fmt.Errorf("invalid destination")
	}

	if pomeriumURL != "" {
		proxyURL, err = url.Parse(pomeriumURL)
		if err != nil {
			return "", nil, fmt.Errorf("invalid pomerium url")
		}
		if proxyURL.Host == "" {
			return "", nil, fmt.Errorf("invalid pomerium url")
		}
	}

	if !strings.Contains(proxyURL.Host, ":") {
		if proxyURL.Scheme == "https" {
			proxyURL.Host = net.JoinHostPort(proxyURL.Host, "443")
		} else {
			proxyURL.Host = net.JoinHostPort(proxyURL.Host, "80")
		}
	}

	return destinationAddr, proxyURL, nil
}
