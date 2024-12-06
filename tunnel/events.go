package tunnel

import (
	"context"

	"github.com/rs/zerolog/log"
)

// EventSink is used to notify on the tunnel state transition
type EventSink interface {
	// OnConnecting is called when listener is accepting a new connection from client
	OnConnecting(context.Context)
	// OnConnected is called when a connection is successfully
	// established to the remote destination via pomerium proxy
	OnConnected(context.Context)
	// OnAuthRequired is called after listener accepted a new connection from client,
	// but has to perform user authentication first
	OnAuthRequired(context.Context, string)
	// OnDisconnected is called when connection to client was closed
	OnDisconnected(context.Context, error)
}

// DiscardEvents returns an event sink that discards all events.
func DiscardEvents() EventSink {
	return discardEvents{}
}

type discardEvents struct{}

// OnConnecting is called when listener is accepting a new connection from client
func (discardEvents) OnConnecting(_ context.Context) {}

// OnConnected is called when a connection is successfully
// established to the remote destination via pomerium proxy
func (discardEvents) OnConnected(_ context.Context) {}

// OnAuthRequired is called after listener accepted a new connection from client,
// but has to perform user authentication first
func (discardEvents) OnAuthRequired(_ context.Context, _ string) {}

// OnDisconnected is called when connection to client was closed
func (discardEvents) OnDisconnected(_ context.Context, _ error) {}

type logEvents struct{}

// LogEvents returns an event sink that logs all events.
func LogEvents() EventSink {
	return logEvents{}
}

func (logEvents) OnConnecting(ctx context.Context) {
	log.Ctx(ctx).Info().Msg("connecting")
}

func (logEvents) OnConnected(ctx context.Context) {
	log.Ctx(ctx).Info().Msg("connected")
}

func (logEvents) OnAuthRequired(ctx context.Context, authURL string) {
	log.Ctx(ctx).Info().Str("auth-url", authURL).Msg("auth required")
}

func (logEvents) OnDisconnected(ctx context.Context, err error) {
	log.Ctx(ctx).Error().Err(err).Msg("disconnected")
}
