package main

import (
	"errors"
	"github.com/mdlayher/vsock"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newDaemonCommand() *cobra.Command {
	daemonCommand := &cobra.Command{
		Use:   "daemon",
		Short: "run the daemon",
		RunE:  daemonAction,
	}
	daemonCommand.Flags().Duration("tick", 3*time.Second, "tick for polling events")
	return daemonCommand
}

func daemonAction(cmd *cobra.Command, args []string) error {
	tick, err := cmd.Flags().GetDuration("tick")
	if err != nil {
		return err
	}
	if tick == 0 {
		return errors.New("tick must be specified")
	}
	if os.Geteuid() != 0 {
		return errors.New("must run as the root")
	}
	logrus.Infof("event tick: %v", tick)

	listen, err := vsock.Dial(vsock.Host, 2222, &vsock.Config{})

	err = SocketListener(listen)
	if err != nil {
		return err
	}
	return nil
}
