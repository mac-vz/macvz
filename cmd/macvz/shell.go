package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/mac-vz/macvz/pkg/sshutil"
	"github.com/mac-vz/macvz/pkg/store"
	"github.com/mattn/go-isatty"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var shellHelp = `Execute shell in MacVZ`

func newShellCommand() *cobra.Command {
	var shellCmd = &cobra.Command{
		Use:               "shell INSTANCE [COMMAND...]",
		Short:             "Execute shell in MacVZ",
		Long:              shellHelp,
		Args:              cobra.MinimumNArgs(1),
		RunE:              shellAction,
		ValidArgsFunction: shellBashComplete,
		SilenceErrors:     true,
	}

	shellCmd.Flags().SetInterspersed(false)

	shellCmd.Flags().String("workdir", "", "working directory")
	return shellCmd
}

func shellAction(cmd *cobra.Command, args []string) error {
	// simulate the behavior of double dash
	newArg := []string{}
	if len(args) >= 2 && args[1] == "--" {
		newArg = append(newArg, args[:1]...)
		newArg = append(newArg, args[2:]...)
		args = newArg
	}
	instName := args[0]

	if len(args) >= 2 {
		switch args[1] {
		case "start", "delete", "shell":
			logrus.Warnf("Perhaps you meant `macvz %s`?", strings.Join(args[1:], " "))
		}
	}

	inst, err := store.Inspect(instName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("instance %q does not exist, run `macvz start %s` to create a new instance", instName, instName)
		}
		return err
	}
	if inst.Status == store.StatusStopped {
		return fmt.Errorf("instance %q is stopped, run `macvz start %s` to start the instance", instName, instName)
	}
	y, err := inst.LoadYAML()
	if err != nil {
		return err
	}

	// When workDir is explicitly set, the shell MUST have workDir as the cwd, or exit with an error.
	//
	// changeDirCmd := "cd workDir || exit 1"                  if workDir != ""
	//              := "cd hostCurrentDir || cd hostHomeDir"   if workDir == ""
	var changeDirCmd string
	workDir, err := cmd.Flags().GetString("workdir")
	if err != nil {
		return err
	}
	if workDir != "" {
		changeDirCmd = fmt.Sprintf("cd %q || exit 1", workDir)
		// FIXME: check whether y.Mounts contains the home, not just len > 0
	} else if len(y.Mounts) > 0 {
		hostCurrentDir, err := os.Getwd()
		if err == nil {
			changeDirCmd = fmt.Sprintf("cd %q", hostCurrentDir)
		} else {
			changeDirCmd = "false"
			logrus.WithError(err).Warn("failed to get the current directory")
		}
		hostHomeDir, err := os.UserHomeDir()
		if err == nil {
			changeDirCmd = fmt.Sprintf("%s || cd %q", changeDirCmd, hostHomeDir)
		} else {
			logrus.WithError(err).Warn("failed to get the home directory")
		}
	} else {
		logrus.Debug("the host home does not seem mounted, so the guest shell will have a different cwd")
	}

	if changeDirCmd == "" {
		changeDirCmd = "false"
	}
	logrus.Debugf("changeDirCmd=%q", changeDirCmd)

	script := fmt.Sprintf("%s ; exec bash --login", changeDirCmd)
	if len(args) > 1 {
		script += fmt.Sprintf(" -c %q", shellescape.QuoteCommand(args[1:]))
	}

	arg0, err := exec.LookPath("ssh")
	if err != nil {
		return err
	}

	sshOpts, err := sshutil.SSHOpts(inst.Dir, true, false)
	if err != nil {
		return err
	}

	sshArgs := sshutil.SSHArgsFromOpts(sshOpts)
	if isatty.IsTerminal(os.Stdout.Fd()) {
		// required for showing the shell prompt: https://stackoverflow.com/a/626574
		sshArgs = append(sshArgs, "-t")
	}
	if _, present := os.LookupEnv("COLORTERM"); present {
		// SendEnv config is cumulative, with already existing options in ssh_config
		sshArgs = append(sshArgs, "-o", "SendEnv=\"COLORTERM\"")
	}
	sshArgs = append(sshArgs, []string{
		"-q",
		fmt.Sprintf("%s", sshutil.SSHRemoteUser(*y.MACAddress)),
		"--",
		script,
	}...)
	sshCmd := exec.Command(arg0, sshArgs...)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr
	logrus.Debugf("executing ssh (may take a long)): %+v", sshCmd.Args)

	return sshCmd.Run()
}

func shellBashComplete(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return bashCompleteInstanceNames(cmd)
}
