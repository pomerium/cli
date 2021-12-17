package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"

	pb "github.com/pomerium/pomerium/pkg/grpc/config"
	"github.com/pomerium/pomerium/pkg/grpc/databroker"
	"github.com/pomerium/pomerium/pkg/grpcutil"
	"github.com/pomerium/pomerium/pkg/protoutil"
)

func init() {
	dbc := dbCommand()
	dbc.AddCommand(dbGetCommand(dbc))
	dbc.AddCommand(dbSetCommand(dbc))
	rootCmd.AddCommand(&dbc.Command)
}

type dbCmd struct {
	cobra.Command
	grpcutil.Options
	serviceURL string
}

type dbGetCmd struct {
	*dbCmd
	outputPath string
	cobra.Command
}

type dbSetCmd struct {
	*dbCmd
	inputPath string
	cobra.Command
}

func dbCommand() *dbCmd {
	cmd := &dbCmd{
		Command: cobra.Command{
			Use:              "db",
			Short:            "databroker config access",
			Args:             cobra.NoArgs,
			TraverseChildren: true,
			Hidden:           true,
		},
	}
	cmd.PersistentPreRunE = cmd.parse
	flags := cmd.PersistentFlags()
	flags.BoolVar(&cmd.InsecureSkipVerify, "insecure-skip-verify", false, "skip TLS verification")
	flags.StringVar(&cmd.CA, "ca", "", "base64 encoded CA PEM cert")
	flags.StringVar(&cmd.CAFile, "ca-file", "", "CA PEM cert")
	flags.StringVar(&cmd.OverrideCertificateName, "cert-name-override", "", "override server cert name")
	flags.DurationVar(&cmd.RequestTimeout, "timeout", time.Second*5, "request timeout")
	flags.BytesBase64Var(&cmd.SignedJWTKey, "shared-secret", nil, "shared secret to access databroker")
	_ = cmd.MarkPersistentFlagRequired("shared-secret")

	flags.StringVar(&cmd.serviceURL, "service-url", "http://localhost:5443", "databroker service url")
	_ = cmd.MarkPersistentFlagRequired("service-url")

	return cmd
}

func dbGetCommand(parent *dbCmd) *cobra.Command {
	cmd := &dbGetCmd{
		dbCmd: parent,
		Command: cobra.Command{
			Use:   "get",
			Short: "get config",
			Args:  cobra.ExactArgs(1),
		},
	}
	cmd.RunE = cmd.exec

	flags := cmd.Flags()
	flags.StringVar(&cmd.outputPath, "out", "-", "output config to, default stdout")

	return &cmd.Command
}

func dbSetCommand(parent *dbCmd) *cobra.Command {
	cmd := &dbSetCmd{
		dbCmd: parent,
		Command: cobra.Command{
			Use:   "set",
			Short: "set config",
			Args:  cobra.ExactArgs(1),
		},
	}
	cmd.RunE = cmd.exec

	flags := cmd.Flags()
	flags.StringVar(&cmd.inputPath, "in", "-", "input from, stdin by default")

	return &cmd.Command
}

func (cmd *dbCmd) parse(c *cobra.Command, args []string) error {
	u, err := url.Parse(cmd.serviceURL)
	if err != nil {
		return fmt.Errorf("parsing service url %s: %w", cmd.serviceURL, err)
	}
	cmd.Address = u
	cmd.ServiceName = "databroker"
	return nil
}

func (cmd *dbCmd) getConn(ctx context.Context) (*grpc.ClientConn, error) {
	return grpcutil.NewGRPCClientConn(ctx, &cmd.Options)
}

func (cmd *dbGetCmd) exec(c *cobra.Command, args []string) error {
	ctx := c.Context()
	conn, err := cmd.getConn(ctx)
	if err != nil {
		return fmt.Errorf("databroker connection: %w", err)
	}

	client := databroker.NewDataBrokerServiceClient(conn)

	cfg := new(pb.Config)
	any := protoutil.NewAny(cfg)
	resp, err := client.Get(ctx, &databroker.GetRequest{
		Type: any.GetTypeUrl(),
		Id:   args[0],
	})
	if err != nil {
		return fmt.Errorf("get config: %w", err)
	}

	if err := resp.GetRecord().GetData().UnmarshalTo(cfg); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}

	txt := protojson.Format(cfg)
	if cmd.outputPath == "-" {
		fmt.Println(txt)
		return nil
	}

	if err := os.WriteFile(cmd.outputPath, []byte(txt), 0600); err != nil {
		return fmt.Errorf("writing to %s: %w", cmd.outputPath, err)
	}

	return nil
}

func (cmd *dbSetCmd) exec(c *cobra.Command, args []string) error {
	ctx := c.Context()
	conn, err := cmd.getConn(ctx)
	if err != nil {
		return fmt.Errorf("databroker connection: %w", err)
	}

	var data []byte
	if cmd.inputPath != "-" {
		data, err = os.ReadFile(cmd.inputPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", cmd.inputPath, err)
		}
	} else {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
	}
	cfg := new(pb.Config)
	if err = protojson.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	client := databroker.NewDataBrokerServiceClient(conn)

	any := protoutil.NewAny(cfg)
	resp, err := client.Put(ctx, &databroker.PutRequest{
		Record: &databroker.Record{
			Type: any.GetTypeUrl(),
			Id:   args[0],
			Data: any,
		},
	})
	if err != nil {
		return fmt.Errorf("set config: %w", err)
	}

	if err := resp.GetRecord().GetData().UnmarshalTo(cfg); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}

	return nil
}
