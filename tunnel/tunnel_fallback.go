package tunnel

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

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
	t.mu.Lock()
	ts := make([]UDPTunneler, len(t.tunnelers))
	copy(ts, t.tunnelers)
	t.mu.Unlock()

	if len(ts) == 0 {
		return fmt.Errorf("%w: no tunnelers defined", errUnsupported)
	}

	err := ts[0].TunnelUDP(ctx, eventSink, local, rawJWT)
	if errors.Is(err, errUnsupported) {
		t.mu.Lock()
		if len(ts) == len(t.tunnelers) && len(ts) > 1 {
			log.Ctx(ctx).Error().Err(err).Msgf("%s tunneler failed, falling back to %s",
				ts[0].Name(), ts[1].Name())
			// try the next tunneler
			t.tunnelers = t.tunnelers[1:]
			t.mu.Unlock()
			return t.TunnelUDP(ctx, eventSink, local, rawJWT)
		}
		t.mu.Unlock()
	}

	return err
}
