package start

import (
	"context"
	"errors"
	"fmt"
	hostagentevents "github.com/mac-vz/macvz/pkg/hostagent/events"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/mac-vz/macvz/pkg/store"
	"github.com/mac-vz/macvz/pkg/store/filenames"
	"github.com/mac-vz/macvz/pkg/vzrun"
	"github.com/mac-vz/macvz/pkg/yaml"
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
	haStdoutPath := filepath.Join(inst.Dir, filenames.HaStdoutLog)
	haStderrPath := filepath.Join(inst.Dir, filenames.HaStderrLog)
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
	begin := time.Now() // used for logrus propagation

	watchErrCh := make(chan error)
	go func() {
		watchErrCh <- watchHostAgentEvents(ctx, inst, haStdoutPath, haStderrPath, begin)
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

func watchHostAgentEvents(ctx context.Context, inst *store.Instance, haStdoutPath, haStderrPath string, begin time.Time) error {
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	var (
		receivedRunningEvent bool
		err                  error
	)
	onEvent := func(ev hostagentevents.Event) bool {
		if len(ev.Status.Errors) > 0 {
			logrus.Errorf("%+v", ev.Status.Errors)
		}
		if ev.Status.Exiting {
			err = fmt.Errorf("exiting, status=%+v (hint: see %q)", ev.Status, haStderrPath)
			return true
		} else if ev.Status.Running {
			receivedRunningEvent = true
			if ev.Status.Degraded {
				logrus.Warnf("DEGRADED. The VM seems running, but file sharing and port forwarding may not work. (hint: see %q)", haStderrPath)
				err = fmt.Errorf("degraded, status=%+v", ev.Status)
				return true
			}

			logrus.Infof("READY. Run `%s` to open the shell.", MacvzShellCmd(inst.Name))
			ShowMessage(inst)
			err = nil
			return true
		}
		return false
	}

	if xerr := hostagentevents.Watch(ctx2, haStdoutPath, haStderrPath, begin, onEvent); xerr != nil {
		return xerr
	}

	if err != nil {
		return err
	}

	if !receivedRunningEvent {
		return errors.New("did not receive an event with the \"running\" status")
	}

	return nil
}

func MacvzShellCmd(instName string) string {
	shellCmd := fmt.Sprintf("macvz shell %s", instName)
	if instName == "default" {
		shellCmd = "macvz"
	}
	return shellCmd
}

func ShowMessage(inst *store.Instance) error {
	//if inst.Message == "" {
	//	return nil
	//}
	//t, err := template.New("message").Parse(inst.Message)
	//if err != nil {
	//	return err
	//}
	//data, err := store.AddGlobalFields(inst)
	//if err != nil {
	//	return err
	//}
	//var b bytes.Buffer
	//if err := t.Execute(&b, data); err != nil {
	//	return err
	//}
	//scanner := bufio.NewScanner(&b)
	//logrus.Infof("Message from the instance %q:", inst.Name)
	//for scanner.Scan() {
	//	// Avoid prepending logrus "INFO" header, for ease of copypasting
	//	fmt.Println(scanner.Text())
	//}
	//if err := scanner.Err(); err != nil {
	//	return err
	//}
	return nil
}
