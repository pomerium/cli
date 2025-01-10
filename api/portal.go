package api

import (
	"context"
	"fmt"

	"github.com/pomerium/cli/proto"
)

func (srv *server) FetchRoutes(
	ctx context.Context,
	req *proto.FetchRoutesRequest,
) (*proto.FetchRoutesResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
