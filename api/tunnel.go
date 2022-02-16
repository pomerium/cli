package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"time"

	"github.com/cenkalti/backoff/v4"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/pomerium/cli/proto"
	"github.com/pomerium/cli/tcptunnel"
)

func newTunnel(conn *pb.Connection, browserCmd string) (Tunnel, string, error) {
	listenAddr := "127.0.0.1:0"
	if conn.ListenAddr != nil {
		listenAddr = *conn.ListenAddr
	}

	pxy, err := getProxy(conn)
	if err != nil {
		return nil, "", fmt.Errorf("cannot determine proxy host: %w", err)
	}

	var tlsCfg *tls.Config
	if pxy.Scheme == "https" {
		tlsCfg, err = getTLSConfig(conn)
		if err != nil {
			return nil, "", fmt.Errorf("tls: %w", err)
		}
	}

	return tcptunnel.New(
		tcptunnel.WithDestinationHost(conn.GetRemoteAddr()),
		tcptunnel.WithProxyHost(pxy.Host),
		tcptunnel.WithTLSConfig(tlsCfg),
		tcptunnel.WithBrowserCommand(browserCmd),
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

func getTLSConfig(conn *pb.Connection) (*tls.Config, error) {
	cfg := &tls.Config{
		//nolint: gosec
		InsecureSkipVerify: conn.GetDisableTlsVerification(),
	}

	if conn.ClientCert != nil {
		if len(conn.ClientCert.Cert) == 0 {
			return nil, fmt.Errorf("client cert: certificate is missing")
		}
		if len(conn.ClientCert.Key) == 0 {
			return nil, fmt.Errorf("client cert: key is missing")
		}
		cert, err := tls.X509KeyPair(conn.ClientCert.Cert, conn.ClientCert.Key)
		if err != nil {
			return nil, fmt.Errorf("client cert: %w", err)
		}
		cfg.Certificates = append(cfg.Certificates, cert)
	}

	if len(conn.GetCaCert()) == 0 {
		return cfg, nil
	}

	rootCA, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("get system cert pool: %w", err)
	}
	if ok := rootCA.AppendCertsFromPEM(conn.GetCaCert()); !ok {
		return nil, fmt.Errorf("failed to append provided certificate")
	}
	cfg.RootCAs = rootCA
	return cfg, nil
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
				log.Printf("failed to accept local connection: %v\n", err)
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
			defer func() { _ = conn.Close() }()

			cEvt := evt.withPeer(conn)
			err := tun.Run(ctx, conn, cEvt)
			if err != nil {
				log.Printf("error serving local connection %s: %v\n", id, err)
			}
			cEvt.OnDisconnected(ctx, err)
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
		log.Printf("failed to send status update %s: %v\n", protojson.Format(upd), err)
	}
}

func (evt *tunnelEvents) onListening(ctx context.Context) {
	if err := evt.Reset(ctx, evt.id); err != nil {
		log.Printf("failed to reset connection history for %s: %v\n", evt.id, err)
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
	evt.update(ctx, &pb.ConnectionStatusUpdate{
		Status: pb.ConnectionStatusUpdate_CONNECTION_STATUS_CONNECTING,
	})
}

// OnConnected is called when a connection is successfully
// established to the remote destination via pomerium proxy
func (evt *tunnelEvents) OnConnected(ctx context.Context) {
	evt.update(ctx, &pb.ConnectionStatusUpdate{
		Status: pb.ConnectionStatusUpdate_CONNECTION_STATUS_CONNECTED,
	})
}

// OnAuthRequired is called after listener accepted a new connection from client,
// but has to perform user authentication first
func (evt *tunnelEvents) OnAuthRequired(ctx context.Context, u string) {
	evt.update(ctx, &pb.ConnectionStatusUpdate{
		Status:  pb.ConnectionStatusUpdate_CONNECTION_STATUS_AUTH_REQUIRED,
		AuthUrl: &u,
	})
}

// OnDisconnected is called when connection to client was closed
func (evt *tunnelEvents) OnDisconnected(ctx context.Context, err error) {
	e := &pb.ConnectionStatusUpdate{
		Status: pb.ConnectionStatusUpdate_CONNECTION_STATUS_DISCONNECTED,
	}
	if err != nil {
		txt := err.Error()
		e.LastError = &txt
	}
	evt.update(ctx, e)
}
