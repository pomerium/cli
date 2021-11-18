package main

import (
	"fmt"
	"net"
	"os"
	"path"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/pomerium/cli/api"
	pb "github.com/pomerium/cli/proto"
)

func init() {
	rootCmd.AddCommand(apiCommand())
}

type apiCmd struct {
	jsonRPCAddr string
	grpcAddr    string
	configPath  string
	browserCmd  string

	cobra.Command
}

func apiCommand() *cobra.Command {
	cmd := &apiCmd{
		Command: cobra.Command{
			Use:    "api",
			Short:  "run api server",
			Hidden: true,
		},
	}
	cmd.RunE = cmd.exec

	cfgDir, err := os.UserConfigDir()
	if err == nil {
		cfgDir = path.Join(cfgDir, "PomeriumDesktop", "config.json")
	}
	flags := cmd.Flags()
	flags.StringVar(&cmd.jsonRPCAddr, "json-addr", "127.0.0.1:8900", "address json api server should listen to")
	flags.StringVar(&cmd.grpcAddr, "grpc-addr", "127.0.0.1:8800", "address json api server should listen to")
	flags.StringVar(&cmd.configPath, "config-path", cfgDir, "path to config file")
	flags.StringVar(&cmd.browserCmd, "browser-cmd", "", "use specific browser app")
	return &cmd.Command
}

func (cmd *apiCmd) makeConfigPath() error {
	if cmd.configPath == "" {
		return fmt.Errorf("config file path could not be determined")
	}

	return os.MkdirAll(path.Dir(cmd.configPath), 0700)
}

func (cmd *apiCmd) exec(c *cobra.Command, args []string) error {
	if err := cmd.makeConfigPath(); err != nil {
		return fmt.Errorf("config %s: %w", cmd.configPath, err)
	}

	srv, err := api.NewServer(c.Context(),
		api.WithConfigProvider(api.FileConfigProvider(cmd.configPath)),
		api.WithBrowserCommand(cmd.browserCmd),
	)
	if err != nil {
		return err
	}

	ctx := c.Context()
	lCfg := new(net.ListenConfig)
	lis, err := lCfg.Listen(ctx, "tcp", cmd.grpcAddr)
	if err != nil {
		return err
	}
	log.Info().Str("address", lis.Addr().String()).Msg("starting gRPC server")

	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(pb.UnaryLog),
	}
	grpcSrv := grpc.NewServer(opts...)
	pb.RegisterConfigServer(grpcSrv, srv)
	pb.RegisterListenerServer(grpcSrv, srv)
	reflection.Register(grpcSrv)
	return grpcSrv.Serve(lis)
}
