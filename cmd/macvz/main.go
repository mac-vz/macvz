package main

import (
	"errors"
	"fmt"
	"github.com/balaji113/macvz/pkg/store/dirnames"
	"github.com/balaji113/macvz/pkg/version"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	DefaultInstanceName = "default"
)

func main() {
	if err := newApp().Execute(); err != nil {
		handleExitCoder(err)
		logrus.Fatal(err)
	}
}

func newApp() *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:     "macvz",
		Short:   "Mac Virtualization",
		Version: strings.TrimPrefix(version.Version, "v"),
		Example: fmt.Sprintf(`  Start the default instance:
  $ macvz start

  Stop the default instance:
  $ macvz stop`),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.PersistentFlags().Bool("debug", false, "debug mode")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		debug, _ := cmd.Flags().GetBool("debug")
		if debug {
			logrus.SetLevel(logrus.DebugLevel)
		}
		if os.Geteuid() == 0 {
			return errors.New("must not run as the root")
		}
		// Make sure either $HOME or $LIMA_HOME is defined, so we don't need
		// to check for errors later
		if _, err := dirnames.MacVZDir(); err != nil {
			return err
		}
		return nil
	}
	rootCmd.AddCommand(
		newStartCommand(),
		newVZCommand(),
		newShellCommand(),
		newStopCommand(),
	)
	return rootCmd
}

type ExitCoder interface {
	error
	ExitCode() int
}

func handleExitCoder(err error) {
	if err == nil {
		return
	}

	if exitErr, ok := err.(ExitCoder); ok {
		os.Exit(exitErr.ExitCode())
		return
	}
}
