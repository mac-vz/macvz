package yaml

import (
	"fmt"
	"net"
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

	for i, p := range y.Provision {
		switch p.Mode {
		case ProvisionModeSystem, ProvisionModeUser:
		default:
			return fmt.Errorf("field `provision[%d].mode` must be either %q or %q",
				i, ProvisionModeSystem, ProvisionModeUser)
		}
	}

	for i, p := range y.Probes {
		switch p.Mode {
		case ProbeModeReadiness:
		default:
			return fmt.Errorf("field `probe[%d].mode` can only be %q",
				i, ProbeModeReadiness)
		}
	}

	for i, rule := range y.PortForwards {
		field := fmt.Sprintf("portForwards[%d]", i)
		if rule.GuestIPMustBeZero && !rule.GuestIP.Equal(net.IPv4zero) {
			return fmt.Errorf("field `%s.guestIPMustBeZero` can only be true when field `%s.guestIP` is 0.0.0.0", field, field)
		}
		if rule.GuestPort != 0 {
			if rule.GuestSocket != "" {
				return fmt.Errorf("field `%s.guestPort` must be 0 when field `%s.guestSocket` is set", field, field)
			}
			if rule.GuestPort != rule.GuestPortRange[0] {
				return fmt.Errorf("field `%s.guestPort` must match field `%s.guestPortRange[0]`", field, field)
			}
			// redundant validation to make sure the error contains the correct field name
			if err := validatePort(field+".guestPort", rule.GuestPort); err != nil {
				return err
			}
		}
		if rule.HostPort != 0 {
			if rule.HostSocket != "" {
				return fmt.Errorf("field `%s.hostPort` must be 0 when field `%s.hostSocket` is set", field, field)
			}
			if rule.HostPort != rule.HostPortRange[0] {
				return fmt.Errorf("field `%s.hostPort` must match field `%s.hostPortRange[0]`", field, field)
			}
			// redundant validation to make sure the error contains the correct field name
			if err := validatePort(field+".hostPort", rule.HostPort); err != nil {
				return err
			}
		}
		for j := 0; j < 2; j++ {
			if err := validatePort(fmt.Sprintf("%s.guestPortRange[%d]", field, j), rule.GuestPortRange[j]); err != nil {
				return err
			}
			if err := validatePort(fmt.Sprintf("%s.hostPortRange[%d]", field, j), rule.HostPortRange[j]); err != nil {
				return err
			}
		}
		if rule.GuestPortRange[0] > rule.GuestPortRange[1] {
			return fmt.Errorf("field `%s.guestPortRange[1]` must be greater than or equal to field `%s.guestPortRange[0]`", field, field)
		}
		if rule.HostPortRange[0] > rule.HostPortRange[1] {
			return fmt.Errorf("field `%s.hostPortRange[1]` must be greater than or equal to field `%s.hostPortRange[0]`", field, field)
		}
		if rule.GuestPortRange[1]-rule.GuestPortRange[0] != rule.HostPortRange[1]-rule.HostPortRange[0] {
			return fmt.Errorf("field `%s.hostPortRange` must specify the same number of ports as field `%s.guestPortRange`", field, field)
		}
		if rule.GuestSocket != "" {
			if !filepath.IsAbs(rule.GuestSocket) {
				return fmt.Errorf("field `%s.guestSocket` must be an absolute path", field)
			}
			if rule.HostSocket == "" && rule.HostPortRange[1]-rule.HostPortRange[0] > 0 {
				return fmt.Errorf("field `%s.guestSocket` can only be mapped to a single port or socket. not a range", field)
			}
		}
		if rule.HostSocket != "" {
			if !filepath.IsAbs(rule.HostSocket) {
				// should be unreachable because FillDefault() will prepend the instance directory to relative names
				return fmt.Errorf("field `%s.hostSocket` must be an absolute path, but is %q", field, rule.HostSocket)
			}
			if rule.GuestSocket == "" && rule.GuestPortRange[1]-rule.GuestPortRange[0] > 0 {
				return fmt.Errorf("field `%s.hostSocket` can only be mapped from a single port or socket. not a range", field)
			}
		}
		if len(rule.HostSocket) >= osutil.UnixPathMax {
			return fmt.Errorf("field `%s.hostSocket` must be less than UNIX_PATH_MAX=%d characters, but is %d",
				field, osutil.UnixPathMax, len(rule.HostSocket))
		}
		if rule.Proto != TCP {
			return fmt.Errorf("field `%s.proto` must be %q", field, TCP)
		}
		// Not validating that the various GuestPortRanges and HostPortRanges are not overlapping. Rules will be
		// processed sequentially and the first matching rule for a guest port determines forwarding behavior.
	}

	return nil
}

func validatePort(field string, port int) error {
	switch {
	case port < 0:
		return fmt.Errorf("field `%s` must be > 0", field)
	case port == 0:
		return fmt.Errorf("field `%s` must be set", field)
	case port == 22:
		return fmt.Errorf("field `%s` must not be 22", field)
	case port > 65535:
		return fmt.Errorf("field `%s` must be < 65536", field)
	}
	return nil
}
