package store

import (
	"errors"
	"github.com/balaji113/macvz/pkg/store/filenames"
	"github.com/balaji113/macvz/pkg/yaml"
	"github.com/docker/go-units"
	"os"
	"path/filepath"
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

	VZScreen int     `json:"VZScreen,omitempty"`
	Errors   []error `json:"errors,omitempty"`
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

	screen := filepath.Join(instDir, filenames.VZScreen)
	_, err = os.Stat(screen)

	if inst.Status == StatusUnknown {
		if err == nil {
			inst.Status = StatusRunning
		} else if err != nil {
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
