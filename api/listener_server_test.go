package api_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/pomerium/cli/api"
	pb "github.com/pomerium/cli/proto"
)

func TestListenerServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := api.NewServer(ctx)
	require.NoError(t, err)

	rec, err := srv.Upsert(ctx, &pb.Record{
		Tags: []string{"test"},
		Conn: &pb.Connection{
			RemoteAddr: "tcp.localhost.pomerium.io:99",
			ListenAddr: proto.String(":0"),
		},
	})
	require.NoError(t, err)
	idA := rec.GetId()
	require.NotEmpty(t, idA)

	status, err := srv.Update(ctx, &pb.ListenerUpdateRequest{
		ConnectionIds: []string{idA},
		Connected:     true,
	})
	require.NoError(t, err)

	cs, there := status.Listeners[idA]
	require.True(t, there)
	require.NotNil(t, cs.ListenAddr)
	require.True(t, cs.Listening)
	require.Nil(t, cs.LastError)
	listenAddr := *cs.ListenAddr

	_, err = net.Listen("tcp", listenAddr)
	assert.Error(t, err)

	for k, sel := range map[string]*pb.Selector{
		"all":    {All: true},
		"by tag": {Tags: []string{"test"}},
		"by id":  {Ids: []string{idA}},
	} {
		status, err = srv.GetStatus(ctx, sel)
		if assert.NoError(t, err, k) && assert.Contains(t, status.Listeners, idA, k) {
			if assert.NotNil(t, status.Listeners[idA].ListenAddr, k) {
				assert.Equal(t, listenAddr, *status.Listeners[idA].ListenAddr, k)
			}
			assert.True(t, status.Listeners[idA].Listening, k)
			assert.Nil(t, status.Listeners[idA].LastError, k)
		}
	}

	// update should be idempotent
	status, err = srv.Update(ctx, &pb.ListenerUpdateRequest{
		ConnectionIds: []string{idA},
		Connected:     true,
	})
	if assert.NoError(t, err) && assert.Contains(t, status.Listeners, idA) {
		assert.True(t, status.Listeners[idA].Listening)
		if assert.NotNil(t, status.Listeners[idA].ListenAddr) {
			assert.Equal(t, listenAddr, *status.Listeners[idA].ListenAddr)
		}
		assert.Nil(t, status.Listeners[idA].LastError)
	}

	status, err = srv.Update(ctx, &pb.ListenerUpdateRequest{
		ConnectionIds: []string{idA},
		Connected:     false,
	})
	if assert.NoError(t, err) && assert.Contains(t, status.Listeners, idA) {
		assert.False(t, status.Listeners[idA].Listening)
		assert.Nil(t, status.Listeners[idA].ListenAddr)
		assert.Nil(t, status.Listeners[idA].LastError)
	}

	status, err = srv.GetStatus(ctx, &pb.Selector{All: true})
	if assert.NoError(t, err) && assert.Contains(t, status.Listeners, idA) {
		assert.False(t, status.Listeners[idA].Listening)
		assert.Nil(t, status.Listeners[idA].ListenAddr)
		assert.Nil(t, status.Listeners[idA].LastError)
	}

	// ensure listener is shut down
	assert.Eventually(t, func() bool {
		conn, err := net.Listen("tcp", listenAddr)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}, time.Second*2, time.Millisecond*100)
}

func TestDeleteActiveListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := api.NewServer(ctx)
	require.NoError(t, err)

	rec, err := srv.Upsert(ctx, &pb.Record{
		Tags: []string{"test"},
		Conn: &pb.Connection{
			RemoteAddr: "tcp.localhost.pomerium.io:99",
			ListenAddr: proto.String(":0"),
		},
	})
	require.NoError(t, err)
	id := rec.GetId()
	require.NotEmpty(t, id)

	status, err := srv.Update(ctx, &pb.ListenerUpdateRequest{
		ConnectionIds: []string{id},
		Connected:     true,
	})
	require.NoError(t, err)

	cs, there := status.Listeners[id]
	require.True(t, there)
	require.NotNil(t, cs.ListenAddr)
	require.True(t, cs.Listening)
	require.Nil(t, cs.LastError)
	listenAddr := *cs.ListenAddr

	canListen := func() bool {
		li, err := net.Listen("tcp", listenAddr)
		if err != nil {
			return false
		}
		li.Close()
		return true
	}

	require.Eventually(t, func() bool {
		return !canListen()
	}, time.Second*2, time.Millisecond*100, "listener should be up")

	rec, err = srv.Upsert(ctx, &pb.Record{
		Tags: []string{"test"},
		Conn: &pb.Connection{
			RemoteAddr: "notactive.localhost.pomerium.io:99",
			ListenAddr: proto.String(":0"),
		},
	})
	require.NoError(t, err)
	idB := rec.GetId()
	require.NotEmpty(t, idB)

	_, err = srv.Delete(ctx, &pb.Selector{
		Ids: []string{id, idB},
	})
	require.NoError(t, err, "can delete")

	// listener should be down
	require.Eventually(t, canListen, time.Second*2, time.Millisecond*100, "listener should be down")

	recs, err := srv.List(ctx, &pb.Selector{All: true})
	require.NoError(t, err, "fetch records")
	require.Empty(t, recs.Records)
}
