//go:build windows

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

var tcpWindowsCmdOptions struct {
	service bool
}

func init() {
	flags := tcpCmd.Flags()
	addTcpFlags(flags)
	flags.BoolVar(&tcpWindowsCmdOptions.service, "service", false, "emulate Windows service mode")
}

type pomeriumCliService struct {
	destination string
}

func (m *pomeriumCliService) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {

	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())

	go runTcp(ctx, m.destination)

	status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				log.Print("Shutting down Pomerium tunnel service...")
				cancel()
				break loop
			default:
				log.Printf("Unexpected service control request #%d", c)
			}
		}
	}

	status <- svc.Status{State: svc.Stopped}
	return false, 0
}

func runService(name string, destination string, isDebug bool) error {
	if isDebug {
		err := debug.Run(name, &pomeriumCliService{destination})
		if err != nil {
			return fmt.Errorf("Error running service in debug mode: %w", err)
		}
	} else {
		err := svc.Run(name, &pomeriumCliService{destination})
		if err != nil {
			return fmt.Errorf("Error running service in Service Control mode: %w", err)
		}
	}
	return nil
}

var tcpCmd = &cobra.Command{
	Use:   "tcp destination",
	Short: "creates a TCP tunnel through Pomerium",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		isService, err := svc.IsWindowsService()
		if err != nil {
			return fmt.Errorf("Could not determine service status: %w", err)
		}

		if isService || *&tcpWindowsCmdOptions.service {
			return runService("Pomerium CLI", args[0], !isService)
		} else {
			return runTcpForever(args[0])
		}
	},
}
