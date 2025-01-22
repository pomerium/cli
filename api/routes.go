package api

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/pomerium/cli/internal/portal"
	pb "github.com/pomerium/cli/proto"
)

func (srv *server) FetchRoutes(
	ctx context.Context,
	req *pb.FetchRoutesRequest,
) (*pb.FetchRoutesResponse, error) {
	tlsConfig, err := getTLSConfig(req)
	if err != nil {
		return nil, err
	}

	p := portal.New(
		portal.WithBrowserCommand(srv.browserCmd),
		portal.WithServiceAccount(srv.serviceAccount),
		portal.WithServiceAccountFile(srv.serviceAccountFile),
		portal.WithTLSConfig(tlsConfig),
	)

	routes, err := p.ListRoutes(ctx, req.GetServerUrl())
	if err != nil {
		return nil, err
	}

	res := new(pb.FetchRoutesResponse)
	for _, route := range routes {
		r := &pb.PortalRoute{
			Id:          route.ID,
			Name:        route.Name,
			Type:        route.Type,
			From:        route.From,
			Description: route.Description,
			LogoUrl:     route.LogoURL,
		}
		if route.ConnectCommand != "" {
			r.ConnectCommand = proto.String(route.ConnectCommand)
		}
		res.Routes = append(res.Routes, r)
	}
	return res, nil
}
