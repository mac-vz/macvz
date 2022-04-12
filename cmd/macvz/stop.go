package main

import (
	"fmt"
	"github.com/mac-vz/macvz/pkg/store"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func newStopCommand() *cobra.Command {
	var stopCmd = &cobra.Command{
		Use:               "stop NAME",
		Short:             fmt.Sprintf("Stop a VM based on the name of macvz instance"),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: stopBashComplete,
		RunE:              stopAction,
	}

	stopCmd.Flags().BoolP("force", "f", false, "force stop the instance")
	return stopCmd
}

func stopAction(cmd *cobra.Command, args []string) error {
	instName := DefaultInstanceName
	if len(args) > 0 {
		instName = args[0]
	}

	inst, err := store.Inspect(instName)
	if err != nil {
		return err
	}

	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return err
	}
	if force {
		stopInstanceForcibly(inst)
	} else {
		err = stopInstanceGracefully(inst)
	}
	return err
}

func stopInstanceGracefully(inst *store.Instance) error {
	if inst.Status != store.StatusRunning {
		return fmt.Errorf("expected status %q, got %q (maybe use `macvz stop -f`?)", store.StatusRunning, inst.Status)
	}

	logrus.Infof("Sending SIGINT to vz process %d", inst.VZPid)
	if err := syscall.Kill(inst.VZPid, syscall.SIGINT); err != nil {
		logrus.Error(err)
	}
	//TODO - Check the log for termination
	return nil
}

func stopInstanceForcibly(inst *store.Instance) {
	if inst.VZPid > 0 {
		logrus.Infof("Sending SIGKILL to the vz process %d", inst.VZPid)
		if err := syscall.Kill(inst.VZPid, syscall.SIGKILL); err != nil {
			logrus.Error(err)
		}
	} else {
		logrus.Info("The host agent process seems already stopped")
	}

	logrus.Infof("Removing *.pid *.sock under %q", inst.Dir)
	fi, err := os.ReadDir(inst.Dir)
	if err != nil {
		logrus.Error(err)
		return
	}
	for _, f := range fi {
		path := filepath.Join(inst.Dir, f.Name())
		if strings.HasSuffix(path, ".pid") || strings.HasSuffix(path, ".sock") {
			logrus.Infof("Removing %q", path)
			if err := os.Remove(path); err != nil {
				logrus.Error(err)
			}
		}
	}
}

func stopBashComplete(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	instances, _ := bashCompleteInstanceNames(cmd)
	return instances, cobra.ShellCompDirectiveDefault
}
