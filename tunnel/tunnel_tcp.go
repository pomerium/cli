package tunnel

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/pomerium/cli/internal/httputil"
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
	ctx = log.Ctx(ctx).With().Str("component", "pick-tcp-tunneler").Logger().WithContext(ctx)

	fallback := &http1tunneler{cfg: tun.cfg}

	// if we're not using TLS, only HTTP1 is supported
	if tun.cfg.tlsConfig == nil {
		log.Ctx(ctx).Info().Msg("tls not enabled, using http1")
		return fallback
	}

	client := &http.Client{
		Transport: httputil.NewLoggingRoundTripper(log.Logger, &http.Transport{
			ForceAttemptHTTP2: true,
			TLSClientConfig:   tun.cfg.tlsConfig,
		}),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+tun.cfg.proxyHost, nil)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to create probe request, falling back to http1")
		return fallback
	}

	res, err := client.Do(req)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to make probe request, falling back to http1")
		return fallback
	}
	res.Body.Close()

	if v := res.Header.Get("Alt-Svc"); strings.Contains(v, "h3") {
		log.Ctx(ctx).Info().Msg("using http3")
		return &http3tunneler{cfg: tun.cfg}
	}

	log.Ctx(ctx).Info().Msg("using http1")
	return fallback
}
