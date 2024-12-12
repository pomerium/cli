//go:build !windows

package main

import (
	"github.com/spf13/cobra"
)

func init() {
	flags := tcpCmd.Flags()
	addTcpFlags(flags)
}

var tcpCmd = &cobra.Command{
	Use:   "tcp destination",
	Short: "creates a TCP tunnel through Pomerium",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTcpForever(args[0])
	},
}
