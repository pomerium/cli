//go:build tools
// +build tools

package cli

import (
	_ "github.com/client9/misspell/cmd/misspell"
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
