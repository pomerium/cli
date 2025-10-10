package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/rs/zerolog/log"
)

type fallbackTCPTunneler struct {
	mu        sync.Mutex
	tunnelers []TCPTunneler
}

func newFallbackTCPTunneler(tunnelers ...TCPTunneler) TCPTunneler {
	return &fallbackTCPTunneler{
		tunnelers: tunnelers,
	}
}

func (*fallbackTCPTunneler) Name() string { return "fallback" }

func (t *fallbackTCPTunneler) TunnelTCP(
	ctx context.Context,
	eventSink EventSink,
	local io.ReadWriter,
	rawJWT string,
) error {
	for i := range t.tunnelers {
		t.mu.Lock()
		tun := t.tunnelers[i]
		t.mu.Unlock()
		if tun == nil {
			continue
		}

		err := tun.TunnelTCP(ctx, eventSink, local, rawJWT)
		if errors.Is(err, errUnsupported) {
			log.Ctx(ctx).Error().Err(err).Msgf("%s tunneler failed", tun.Name())
			t.mu.Lock()
			t.tunnelers[i] = nil
			t.mu.Unlock()
			continue
		}

		return err
	}

	return fmt.Errorf("%w: no tunnelers defined", errUnsupported)
}

type fallbackUDPTunneler struct {
	mu        sync.Mutex
	tunnelers []UDPTunneler
}

func newFallbackUDPTunneler(tunnelers ...UDPTunneler) UDPTunneler {
	return &fallbackUDPTunneler{
		tunnelers: tunnelers,
	}
}

func (*fallbackUDPTunneler) Name() string { return "fallback" }

func (t *fallbackUDPTunneler) TunnelUDP(
	ctx context.Context,
	eventSink EventSink,
	local UDPDatagramReaderWriter,
	rawJWT string,
) error {
	for i := range t.tunnelers {
		t.mu.Lock()
		tun := t.tunnelers[i]
		t.mu.Unlock()
		if tun == nil {
			continue
		}

		err := tun.TunnelUDP(ctx, eventSink, local, rawJWT)
		if errors.Is(err, errUnsupported) {
			log.Ctx(ctx).Error().Err(err).Msgf("%s tunneler failed", tun.Name())
			t.mu.Lock()
			t.tunnelers[i] = nil
			t.mu.Unlock()
			continue
		}

		return err
	}

	return fmt.Errorf("%w: no tunnelers defined", errUnsupported)
}
