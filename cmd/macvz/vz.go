package main

import (
	"fmt"
	"github.com/balaji113/macvz/pkg/hostagent"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"io"
	"os"
	"os/signal"
)

func newVZCommand() *cobra.Command {
	var vzCommand = &cobra.Command{
		Use:               "vz NAME",
		Short:             fmt.Sprintf("Start a VM based on the name of macvz instance"),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: vzBashComplete,
		RunE:              vzAction,
	}

	return vzCommand
}

func vzBashComplete(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	instances, _ := bashCompleteInstanceNames(cmd)
	return instances, cobra.ShellCompDirectiveDefault
}

func vzAction(cmd *cobra.Command, args []string) error {
	instName := args[0]
	stderr := &syncWriter{w: cmd.ErrOrStderr()}
	initLogrus(stderr)

	sigintCh := make(chan os.Signal, 1)
	signal.Notify(sigintCh, os.Interrupt)
	agent, err := hostagent.New(instName, sigintCh)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	if err != nil {
		return err
	}
	return agent.Run(ctx)
}

// syncer is implemented by *os.File
type syncer interface {
	Sync() error
}

type syncWriter struct {
	w io.Writer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	written, err := w.w.Write(p)
	if err == nil {
		if s, ok := w.w.(syncer); ok {
			_ = s.Sync()
		}
	}
	return written, err
}

func initLogrus(stderr io.Writer) {
	logrus.SetOutput(stderr)
	// JSON logs are parsed in pkg/hostagent/events.Watcher()
	logrus.SetFormatter(new(logrus.JSONFormatter))
	logrus.SetLevel(logrus.DebugLevel)
}
