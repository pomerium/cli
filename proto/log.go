package proto

import (
	context "context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func appendProto(evt *zerolog.Event, key string, obj interface{}) *zerolog.Event {
	if obj == nil {
		return evt.Str(key, "nil")
	}
	m, ok := obj.(protoreflect.ProtoMessage)
	if !ok {
		return evt.Str("key", "not a proto")
	}

	data, err := protojson.Marshal(m)
	if err != nil {
		return evt.AnErr(fmt.Sprintf("%s_json", key), err)
	}
	return evt.RawJSON(key, data)
}

func UnaryLog(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	var evt *zerolog.Event

	res, err := handler(ctx, req)
	if status.Code(err) != codes.OK {
		evt = log.Error().Err(err)
	} else {
		evt = log.Info()
	}

	appendProto(
		appendProto(evt, "req", req),
		"res", res,
	).Msg(info.FullMethod)

	return res, err
}
