package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/pomerium/cli/proto"
	"github.com/pomerium/cli/tunnel"
)

func newTunnel(conn *pb.Connection, browserCmd, serviceAccount, serviceAccountFile string) (Tunnel, string, error) {
	listenAddr := "127.0.0.1:0"
	if conn.ListenAddr != nil {
		listenAddr = *conn.ListenAddr
	}

	destinationAddr, proxyURL, err := tunnel.ParseURLs(conn.GetRemoteAddr(), conn.GetPomeriumUrl())
	if err != nil {
		return nil, "", err
	}

	var tlsCfg *tls.Config
	if proxyURL.Scheme == "https" {
		tlsCfg, err = getTLSConfig(conn)
		if err != nil {
			return nil, "", fmt.Errorf("tls: %w", err)
		}
	}

	return tunnel.New(
		tunnel.WithDestinationHost(destinationAddr),
		tunnel.WithProxyHost(proxyURL.Host),
		tunnel.WithServiceAccount(serviceAccount),
		tunnel.WithServiceAccountFile(serviceAccountFile),
		tunnel.WithTLSConfig(tlsCfg),
		tunnel.WithBrowserCommand(browserCmd),
	), listenAddr, nil
}

func getProxy(conn *pb.Connection) (*url.URL, error) {
	host, _, err := net.SplitHostPort(conn.GetRemoteAddr())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", conn.GetRemoteAddr(), err)
	}
	if host == "" {
		return nil, errors.New("remote host is required")
	}

	if conn.PomeriumUrl == nil {
		return &url.URL{
			Scheme: "https",
			Host:   net.JoinHostPort(host, "443"),
		}, nil
	}

	u, err := url.Parse(conn.GetPomeriumUrl())
	if err != nil {
		return nil, fmt.Errorf("invalid pomerium url: %w", err)
	}
	if u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid pomerium url: %q", conn.GetPomeriumUrl())
	}
	if u.Host == u.Hostname() {
		if u.Scheme == "https" {
			u.Host = net.JoinHostPort(u.Host, "443")
		} else {
			u.Host = net.JoinHostPort(u.Host, "80")
		}
	}

	return u, nil
}

func tunnelAcceptLoop(ctx context.Context, id string, li net.Listener, tun Tunnel, b EventBroadcaster) {
	evt := &tunnelEvents{EventBroadcaster: b, id: id}
	evt.onListening(ctx)

	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = 0

	for {
		c, err := li.Accept()
		if err != nil {
			// canceled, so ignore the error and return
			if ctx.Err() != nil {
				evt.onTunnelClosed()
				return
			}

			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				log.Ctx(ctx).Error().Err(err).Msg("failed to accept local connection")
				select {
				case <-time.After(bo.NextBackOff()):
				case <-ctx.Done():
					return
				}
				continue
			}
		}
		bo.Reset()

		go func(conn net.Conn) {
			defer conn.Close()

			cEvt := evt.withPeer(conn)
			err := tun.Run(ctx, conn, cEvt)
			if err != nil {
				log.Ctx(ctx).Error().Err(err).Msg("error serving local connection")
			}
		}(c)
	}
}

type tunnelEvents struct {
	EventBroadcaster
	id   string
	peer *string
}

func (evt *tunnelEvents) withPeer(conn net.Conn) *tunnelEvents {
	ne := *evt
	ne.peer = proto.String(conn.RemoteAddr().String())
	return &ne
}

func (evt *tunnelEvents) update(ctx context.Context, upd *pb.ConnectionStatusUpdate) {
	upd.Ts = timestamppb.Now()
	upd.PeerAddr = evt.peer
	upd.Id = evt.id
	if err := evt.Update(ctx, upd); err != nil {
		log.Ctx(ctx).Error().Err(err).Str("update", protojson.Format(upd)).Msg("failed to send status update")
	}
}

func (evt *tunnelEvents) onListening(ctx context.Context) {
	if err := evt.Reset(ctx, evt.id); err != nil {
		log.Ctx(ctx).Error().Err(err).Str("event-id", evt.id).Msg("failed to reset connection history")
	}
	evt.update(ctx, &pb.ConnectionStatusUpdate{
		Status: pb.ConnectionStatusUpdate_CONNECTION_STATUS_LISTENING,
	})
}

func (evt *tunnelEvents) onTunnelClosed() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	evt.update(ctx, &pb.ConnectionStatusUpdate{
		Status: pb.ConnectionStatusUpdate_CONNECTION_STATUS_CLOSED,
	})
}

// OnConnecting is called when listener is accepting a new connection from client
func (evt *tunnelEvents) OnConnecting(ctx context.Context) {
	log.Ctx(ctx).Info().Msg("connecting")
	evt.update(ctx, &pb.ConnectionStatusUpdate{
		Status: pb.ConnectionStatusUpdate_CONNECTION_STATUS_CONNECTING,
	})
}

// OnConnected is called when a connection is successfully
// established to the remote destination via pomerium proxy
func (evt *tunnelEvents) OnConnected(ctx context.Context) {
	log.Ctx(ctx).Info().Msg("connected")
	evt.update(ctx, &pb.ConnectionStatusUpdate{
		Status: pb.ConnectionStatusUpdate_CONNECTION_STATUS_CONNECTED,
	})
}

// OnAuthRequired is called after listener accepted a new connection from client,
// but has to perform user authentication first
func (evt *tunnelEvents) OnAuthRequired(ctx context.Context, u string) {
	log.Ctx(ctx).Info().Str("auth-url", u).Msg("auth required")
	evt.update(ctx, &pb.ConnectionStatusUpdate{
		Status:  pb.ConnectionStatusUpdate_CONNECTION_STATUS_AUTH_REQUIRED,
		AuthUrl: &u,
	})
}

// OnDisconnected is called when connection to client was closed
func (evt *tunnelEvents) OnDisconnected(ctx context.Context, err error) {
	log.Ctx(ctx).Error().Err(err).Msg("disconnected")
	e := &pb.ConnectionStatusUpdate{
		Status: pb.ConnectionStatusUpdate_CONNECTION_STATUS_DISCONNECTED,
	}
	if err != nil {
		txt := err.Error()
		e.LastError = &txt
	}
	evt.update(ctx, e)
}
