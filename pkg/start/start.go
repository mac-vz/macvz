package start

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/balaji113/macvz/pkg/store"
	"github.com/balaji113/macvz/pkg/store/filenames"
	"github.com/balaji113/macvz/pkg/vzrun"
	"github.com/balaji113/macvz/pkg/yaml"
	"github.com/sirupsen/logrus"
)

func ensureDisk(ctx context.Context, instName, instDir string, y *yaml.MacVZYaml) error {
	qCfg := vzrun.Config{
		Name:        instName,
		InstanceDir: instDir,
		MacVZYaml:   y,
	}
	if err := vzrun.EnsureDisk(ctx, qCfg); err != nil {
		return err
	}

	return nil
}

func Start(ctx context.Context, inst *store.Instance) error {
	vzPid := filepath.Join(inst.Dir, filenames.VZPid)
	if _, err := os.Stat(vzPid); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("instance %q seems running (hint: remove %q if the instance is not actually running)", inst.Name, vzPid)
	}

	y, err := inst.LoadYAML()
	if err != nil {
		return err
	}

	if err := ensureDisk(ctx, inst.Name, inst.Dir, y); err != nil {
		return err
	}
	if err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	haStdoutPath := filepath.Join(inst.Dir, filenames.VZStdoutLog)
	haStderrPath := filepath.Join(inst.Dir, filenames.VZStderrLog)
	if err := os.RemoveAll(haStdoutPath); err != nil {
		return err
	}
	if err := os.RemoveAll(haStderrPath); err != nil {
		return err
	}
	haStdoutW, err := os.Create(haStdoutPath)
	if err != nil {
		return err
	}
	// no defer haStdoutW.Close()
	haStderrW, err := os.Create(haStderrPath)
	if err != nil {
		return err
	}
	// no defer haStderrW.Close()

	var args []string
	if logrus.GetLevel() >= logrus.DebugLevel {
		args = append(args, "--debug")
	}
	args = append(args, "vz", inst.Name)
	vzCmd := exec.CommandContext(ctx, self, args...)

	vzCmd.Stdout = haStdoutW
	vzCmd.Stderr = haStderrW

	// used for logrus propagation

	if err := vzCmd.Start(); err != nil {
		return err
	}

	if err := waitHostAgentStart(ctx, vzPid, haStderrPath); err != nil {
		return err
	}

	watchErrCh := make(chan error)
	go func() {
		//watchErrCh <- watchHostAgentEvents(ctx, inst, haStdoutPath, haStderrPath, begin)
		close(watchErrCh)
	}()
	waitErrCh := make(chan error)
	go func() {
		waitErrCh <- vzCmd.Wait()
		close(waitErrCh)
	}()

	select {
	case watchErr := <-watchErrCh:
		// watchErr can be nil
		return watchErr
		// leave the hostagent process running
	case waitErr := <-waitErrCh:
		// waitErr should not be nil
		return fmt.Errorf("VZ process has exited: %w", waitErr)
	}
}

func waitHostAgentStart(ctx context.Context, screenFile, haStderrPath string) error {
	begin := time.Now()
	deadlineDuration := 15 * time.Second
	deadline := begin.Add(deadlineDuration)
	for {
		if _, err := os.Stat(screenFile); !errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("hostagent (%q) did not start up in %v (hint: see %q)", screenFile, deadlineDuration, haStderrPath)
		}
	}
}
