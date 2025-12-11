package api

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/pomerium/cli/proto"
)

func (s *server) Update(_ context.Context, req *pb.ListenerUpdateRequest) (*pb.ListenerStatusResponse, error) {
	s.Lock()
	defer s.Unlock()

	var fn func(ids []string) (map[string]*pb.ListenerStatus, error)
	if req.Connected {
		fn = s.connectLocked
	} else {
		fn = s.disconnectLocked
	}

	listeners, err := fn(req.GetConnectionIds())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.ListenerStatusResponse{Listeners: listeners}, nil
}

func (s *server) connectLocked(ids []string) (map[string]*pb.ListenerStatus, error) {
	listeners := make(map[string]*pb.ListenerStatus, len(ids))

	for _, id := range ids {
		status := s.GetListenerStatus(id)
		if status.Listening {
			listeners[id] = status
			continue
		}

		addr, err := s.connectTunnelLocked(id)
		if err != nil {
			txt := err.Error()
			listeners[id] = &pb.ListenerStatus{LastError: &txt}
			continue
		}

		concreteAddr := addr.String()
		listeners[id] = &pb.ListenerStatus{
			Listening:  true,
			ListenAddr: &concreteAddr,
		}
	}

	return listeners, nil
}

func (s *server) connectTunnelLocked(id string) (net.Addr, error) {
	rec, there := s.byID[id]
	if !there {
		return nil, errNotFound
	}

	tun, listenAddr, err := newTunnel(rec.GetConn(), s.browserCmd, s.serviceAccount, s.serviceAccountFile)
	if err != nil {
		return nil, err
	}

	if rec.GetConn().GetProtocol() == pb.Protocol_UDP {
		return s.connectUDPTunnelLocked(id, tun, listenAddr)
	}

	return s.connectTCPTunnelLocked(id, tun, listenAddr)
}

func (s *server) connectTCPTunnelLocked(id string, tun Tunnel, listenAddr string) (net.Addr, error) {
	ctx, cancel := context.WithCancel(context.Background())
	lc := new(net.ListenConfig)
	li, err := lc.Listen(ctx, "tcp", listenAddr)
	if err != nil {
		_ = s.EventBroadcaster.Update(ctx, &pb.ConnectionStatusUpdate{
			Id:        id,
			LastError: proto.String(fmt.Errorf("listen: %w", err).Error()),
			Ts:        timestamppb.Now(),
		})
		cancel()
		return nil, err
	}

	if err = s.SetListening(id, cancel, li.Addr().String()); err != nil {
		_ = s.EventBroadcaster.Update(ctx, &pb.ConnectionStatusUpdate{
			Id:        id,
			LastError: proto.String(fmt.Errorf("SetListening: %w", err).Error()),
			Ts:        timestamppb.Now(),
		})
		cancel()
		return nil, err
	}
	go tunnelAcceptLoop(ctx, id, li, tun, s.EventBroadcaster)
	go onContextCancel(ctx, li)

	return li.Addr(), nil
}

func (s *server) connectUDPTunnelLocked(id string, tun Tunnel, listenAddr string) (net.Addr, error) {
	ctx, cancel := context.WithCancel(context.Background())

	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		_ = s.EventBroadcaster.Update(ctx, &pb.ConnectionStatusUpdate{
			Id:        id,
			LastError: proto.String(fmt.Errorf("ResolveUDPAddr: %w", err).Error()),
			Ts:        timestamppb.Now(),
		})
		cancel()
		return nil, err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		_ = s.EventBroadcaster.Update(ctx, &pb.ConnectionStatusUpdate{
			Id:        id,
			LastError: proto.String(fmt.Errorf("ListenUDP: %w", err).Error()),
			Ts:        timestamppb.Now(),
		})
		cancel()
		return nil, err
	}
	context.AfterFunc(ctx, func() { _ = conn.Close() })

	go func() {
		defer cancel()
		evt := &tunnelEvents{EventBroadcaster: s.EventBroadcaster, id: id}
		defer evt.onTunnelClosed()
		evt.onListening(ctx)

		err := tun.RunUDPSessionManager(ctx, conn, evt)
		if err != nil {
			log.Ctx(ctx).Error().Err(err).Msg("error serving local connection")
		}
	}()

	return addr, nil
}

func onContextCancel(ctx context.Context, cl io.Closer) {
	<-ctx.Done()
	_ = cl.Close()
}

func (s *server) disconnectLocked(ids []string) (map[string]*pb.ListenerStatus, error) {
	listeners := make(map[string]*pb.ListenerStatus, len(ids))

	for _, id := range ids {
		if err := s.SetNotListening(id); err != nil {
			txt := err.Error()
			listeners[id] = &pb.ListenerStatus{LastError: &txt}
		} else {
			listeners[id] = s.GetListenerStatus(id)
		}
	}

	return listeners, nil
}

func (s *server) StatusUpdates(req *pb.StatusUpdatesRequest, upd pb.Listener_StatusUpdatesServer) error {
	ch, err := s.Subscribe(upd.Context(), req.ConnectionId)
	if err != nil {
		return err
	}

	for u := range ch {
		if err := upd.Send(u); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) GetStatus(_ context.Context, sel *pb.Selector) (*pb.ListenerStatusResponse, error) {
	s.RLock()
	defer s.RUnlock()

	recs, err := s.listLocked(sel)
	if err != nil {
		return nil, err
	}

	listeners := make(map[string]*pb.ListenerStatus, len(recs))
	for _, r := range recs {
		listeners[r.GetId()] = s.GetListenerStatus(r.GetId())
	}

	return &pb.ListenerStatusResponse{Listeners: listeners}, nil
}
