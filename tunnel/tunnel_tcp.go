package tunnel

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
)

// A TCPTunneler tunnels TCP traffic.
type TCPTunneler interface {
	TunnelTCP(
		ctx context.Context,
		eventSink EventSink,
		local io.ReadWriter,
		rawJWT string,
	) error
}

// PickTCPTunneler picks a tcp tunneler for the given proxy.
func (tun *Tunnel) pickTCPTunneler(ctx context.Context) TCPTunneler {
	fallback := &http1tunneler{cfg: tun.cfg}

	// if we're not using TLS, only HTTP1 is supported
	if tun.cfg.tlsConfig == nil {
		log.Println("pick-tcp-tunneler: tls not enabled, using http1")
		return fallback
	}

	client := &http.Client{
		Transport: &http.Transport{
			ForceAttemptHTTP2: true,
			TLSClientConfig:   tun.cfg.tlsConfig,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+tun.cfg.proxyHost, nil)
	if err != nil {
		log.Println("pick-tcp-tunneler: failed to create probe request, falling back to http1", err)
		return fallback
	}

	res, err := client.Do(req)
	if err != nil {
		log.Println("pick-tcp-tunneler: failed to make probe request, falling back to http1", err)
		return fallback
	}
	res.Body.Close()

	if v := res.Header.Get("Alt-Svc"); strings.Contains(v, "h3") {
		log.Println("pick-tcp-tunneler: using http3")
		return &http3tunneler{cfg: tun.cfg}
	} else if res.ProtoMajor == 2 {
		log.Println("pick-tcp-tunneler: using http2")
		return &http2tunneler{cfg: tun.cfg}
	}

	log.Println("pick-tcp-tunneler: using http1")
	return fallback
}
