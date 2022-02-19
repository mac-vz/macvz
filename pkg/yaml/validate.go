package yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"errors"

	"github.com/balaji113/macvz/pkg/localpathutil"
	"github.com/balaji113/macvz/pkg/osutil"
	"github.com/docker/go-units"
)

func Validate(y MacVZYaml, warn bool) error {

	if len(y.Images) == 0 {
		return errors.New("field `images` must be set")
	}
	for i, f := range y.Images {

		if !strings.Contains(f.Kernel, "://") {
			if _, err := localpathutil.Expand(f.Kernel); err != nil {
				return fmt.Errorf("field `images[%d].kernal` refers to an invalid local file path: %q: %w", i, f.Kernel, err)
			}
		}
		if !strings.Contains(f.Base, "://") {
			if _, err := localpathutil.Expand(f.Base); err != nil {
				return fmt.Errorf("field `images[%d].base` refers to an invalid local file path: %q: %w", i, f.Base, err)
			}
		}
		if !strings.Contains(f.Initram, "://") {
			if _, err := localpathutil.Expand(f.Initram); err != nil {
				return fmt.Errorf("field `images[%d].initram` refers to an invalid local file path: %q: %w", i, f.Initram, err)
			}
		}
		switch f.Arch {
		case X8664, AARCH64:
		default:
			return fmt.Errorf("field `images.arch` must be %q or %q, got %q", X8664, AARCH64, f.Arch)
		}
	}

	if *y.CPUs == 0 {
		return errors.New("field `cpus` must be set")
	}

	if _, err := units.RAMInBytes(*y.Memory); err != nil {
		return fmt.Errorf("field `memory` has an invalid value: %w", err)
	}

	if _, err := units.RAMInBytes(*y.Disk); err != nil {
		return fmt.Errorf("field `memory` has an invalid value: %w", err)
	}

	u, err := osutil.MacVZUser(false)
	if err != nil {
		return fmt.Errorf("internal error (not an error of YAML): %w", err)
	}
	// reservedHome is the home directory defined in "cidata.iso:/user-data"
	reservedHome := fmt.Sprintf("/home/%s.linux", u.Username)

	for i, f := range y.Mounts {
		if !filepath.IsAbs(f.Location) && !strings.HasPrefix(f.Location, "~") {
			return fmt.Errorf("field `mounts[%d].location` must be an absolute path, got %q",
				i, f.Location)
		}
		loc, err := localpathutil.Expand(f.Location)
		if err != nil {
			return fmt.Errorf("field `mounts[%d].location` refers to an unexpandable path: %q: %w", i, f.Location, err)
		}
		switch loc {
		case "/", "/bin", "/dev", "/etc", "/home", "/opt", "/sbin", "/tmp", "/usr", "/var":
			return fmt.Errorf("field `mounts[%d].location` must not be a system path such as /etc or /usr", i)
		case reservedHome:
			return fmt.Errorf("field `mounts[%d].location` is internally reserved", i)
		}

		st, err := os.Stat(loc)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("field `mounts[%d].location` refers to an inaccessible path: %q: %w", i, f.Location, err)
			}
		} else if !st.IsDir() {
			return fmt.Errorf("field `mounts[%d].location` refers to a non-directory path: %q: %w", i, f.Location, err)
		}
	}
	return nil
}
