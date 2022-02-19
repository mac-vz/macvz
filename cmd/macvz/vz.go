package main

import (
	"fmt"
	"github.com/balaji113/macvz/pkg/vzrun"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"io"
)

func newVZCommand() *cobra.Command {
	var vzCommand = &cobra.Command{
		Use:               "vz NAME",
		Short:             fmt.Sprintf("Start a VM based on the name of macvz instance"),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: startBashComplete,
		RunE:              vzAction,
	}

	return vzCommand
}

func vzAction(cmd *cobra.Command, args []string) error {
	instName := args[0]

	stderr := &syncWriter{w: cmd.ErrOrStderr()}
	initLogrus(stderr)

	initialize, err := vzrun.Initialize(instName)
	if err != nil {
		return err
	}

	return vzrun.Run(*initialize)
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
