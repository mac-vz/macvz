package main

import (
	"errors"
	"github.com/mac-vz/macvz/pkg/guestagent"
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

	newTicker := func() (<-chan time.Time, func()) {
		// TODO: use an equivalent of `bpftrace -e 'tracepoint:syscalls:sys_*_bind { printf("tick\n"); }')`,
		// without depending on `bpftrace` binary.
		// The agent binary will need CAP_BPF file cap.
		ticker := time.NewTicker(tick)
		return ticker.C, ticker.Stop
	}

	agent, err := guestagent.New(newTicker, listen, tick*20)
	if err != nil {
		return err
	}
	logrus.Println("Serving at vsock...")
	logrus.Println("Publishing info...")
	agent.PublishInfo()
	logrus.Println("Published info...")
	logrus.Println("Sending Events...")
	agent.ListenAndSendEvents()
	logrus.Println("Stopped sending events")
	return nil
}
