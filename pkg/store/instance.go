package store

import (
	"errors"
	"github.com/docker/go-units"
	"github.com/mac-vz/macvz/pkg/store/filenames"
	"github.com/mac-vz/macvz/pkg/yaml"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type Status = string

const (
	StatusUnknown Status = ""
	StatusBroken  Status = "Broken"
	StatusStopped Status = "Stopped"
	StatusRunning Status = "Running"
)

type Instance struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Dir    string `json:"dir"`
	CPUs   int    `json:"cpus,omitempty"`
	Memory int64  `json:"memory,omitempty"` // bytes
	Disk   int64  `json:"disk,omitempty"`   // bytes

	VZPid  int     `json:"VZPid,omitempty"`
	Errors []error `json:"errors,omitempty"`
}

func (inst *Instance) LoadYAML() (*yaml.MacVZYaml, error) {
	if inst.Dir == "" {
		return nil, errors.New("inst.Dir is empty")
	}
	yamlPath := filepath.Join(inst.Dir, filenames.MacVZYAML)
	return LoadYAMLByFilePath(yamlPath)
}

// Inspect returns err only when the instance does not exist (os.ErrNotExist).
// Other errors are returned as *Instance.Errors
func Inspect(instName string) (*Instance, error) {
	inst := &Instance{
		Name:   instName,
		Status: StatusUnknown,
	}
	// InstanceDir validates the instName but does not check whether the instance exists
	instDir, err := InstanceDir(instName)
	if err != nil {
		return nil, err
	}
	yamlPath := filepath.Join(instDir, filenames.MacVZYAML)
	y, err := LoadYAMLByFilePath(yamlPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		inst.Errors = append(inst.Errors, err)
		return inst, nil
	}
	inst.Dir = instDir
	inst.CPUs = *y.CPUs
	memory, err := units.RAMInBytes(*y.Memory)
	if err == nil {
		inst.Memory = memory
	}
	disk, err := units.RAMInBytes(*y.Disk)
	if err == nil {
		inst.Disk = disk
	}

	vzPidFile := filepath.Join(instDir, filenames.VZPid)
	inst.VZPid, err = ReadPIDFile(vzPidFile)

	_, err = os.Stat(vzPidFile)

	if inst.Status == StatusUnknown {
		if inst.VZPid > 0 && err == nil {
			inst.Status = StatusRunning
		} else if inst.VZPid == 0 {
			inst.Status = StatusStopped
		}
	}

	return inst, nil
}

type FormatData struct {
	Instance
	HostOS       string
	HostArch     string
	LimaHome     string
	IdentityFile string
}

func ReadPIDFile(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, err
	}
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			_ = os.Remove(path)
			return 0, nil
		}
		// We may not have permission to send the signal (e.g. to network daemon running as root).
		// But if we get a permissions error, it means the process is still running.
		if !errors.Is(err, os.ErrPermission) {
			return 0, err
		}
	}
	return pid, nil
}
