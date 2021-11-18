package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	pb "github.com/pomerium/cli/proto"
)

func TestGetProxy(t *testing.T) {
	for label, tc := range map[string]struct {
		*pb.Connection
		url         string
		expectError bool
	}{
		"no address": {
			&pb.Connection{},
			"",
			true,
		},
		"no port in address": {
			&pb.Connection{
				RemoteAddr: "localhost",
			},
			"",
			true,
		},
		"no host in address": {
			&pb.Connection{
				RemoteAddr: ":9999",
			},
			"",
			true,
		},
		"normal": {
			&pb.Connection{
				RemoteAddr: "tcp.localhost.pomerium.io:99",
			},
			"https://tcp.localhost.pomerium.io:443",
			false,
		},
		"custom https proxy": {
			&pb.Connection{
				RemoteAddr:  "tcp.localhost.pomerium.io:99",
				PomeriumUrl: proto.String("https://localhost"),
			},
			"https://localhost:443",
			false,
		},
		"custom http proxy": {
			&pb.Connection{
				RemoteAddr:  "tcp.localhost.pomerium.io:99",
				PomeriumUrl: proto.String("http://localhost"),
			},
			"http://localhost:80",
			false,
		},
		"custom https proxy port": {
			&pb.Connection{
				RemoteAddr:  "tcp.localhost.pomerium.io:99",
				PomeriumUrl: proto.String("https://localhost:5443"),
			},
			"https://localhost:5443",
			false,
		},
		"custom http proxy port": {
			&pb.Connection{
				RemoteAddr:  "tcp.localhost.pomerium.io:99",
				PomeriumUrl: proto.String("http://localhost:8080"),
			},
			"http://localhost:8080",
			false,
		},
		"invalid url": {
			&pb.Connection{
				RemoteAddr:  "tcp.localhost.pomerium.io:99",
				PomeriumUrl: proto.String("localhost:7777"),
			},
			"",
			true,
		},
		"empty proxy url": {
			&pb.Connection{
				RemoteAddr:  "tcp.localhost.pomerium.io:99",
				PomeriumUrl: proto.String(""),
			},
			"",
			true,
		},
	} {
		url, err := getProxy(tc.Connection)
		if tc.expectError {
			assert.Error(t, err, label)
			continue
		}
		if assert.NoError(t, err, label) {
			assert.Equal(t, tc.url, url.String(), label)
		}
	}
}
