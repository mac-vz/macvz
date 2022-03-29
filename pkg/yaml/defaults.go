package yaml

import (
	"bytes"
	"fmt"
	"github.com/balaji113/macvz/pkg/guestagent/api"
	"github.com/balaji113/macvz/pkg/osutil"
	"github.com/balaji113/macvz/pkg/store/filenames"
	"github.com/balaji113/macvz/pkg/vz-wrapper"
	"github.com/sirupsen/logrus"
	"github.com/xorcare/pointer"
	"net"
	"os"
	osuser "os/user"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
)

func FillDefault(y, d, o *MacVZYaml, filePath string) {
	defaultArch := pointer.String(ResolveArch())

	y.Images = append(append(o.Images, y.Images...), d.Images...)
	for i := range y.Images {
		img := &y.Images[i]
		if img.Arch == "" {
			img.Arch = *defaultArch
		}
	}

	if y.CPUs == nil {
		y.CPUs = d.CPUs
	}
	if o.CPUs != nil {
		y.CPUs = o.CPUs
	}
	if y.CPUs == nil || *y.CPUs == 0 {
		y.CPUs = pointer.Int(4)
	}

	if y.Memory == nil {
		y.Memory = d.Memory
	}
	if o.Memory != nil {
		y.Memory = o.Memory
	}
	if y.Memory == nil || *y.Memory == "" {
		y.Memory = pointer.String("4GiB")
	}

	if y.Disk == nil {
		y.Disk = d.Disk
	}
	if o.Disk != nil {
		y.Disk = o.Disk
	}
	if y.Disk == nil || *y.Disk == "" {
		y.Disk = pointer.String("100GiB")
	}

	if y.MACAddress == nil || *y.MACAddress == "" {
		y.MACAddress = pointer.String(vz.NewRandomLocallyAdministeredMACAddress().String())
	}

	y.Provision = append(append(o.Provision, y.Provision...), d.Provision...)
	for i := range y.Provision {
		provision := &y.Provision[i]
		if provision.Mode == "" {
			provision.Mode = ProvisionModeSystem
		}
	}

	y.Probes = append(append(o.Probes, y.Probes...), d.Probes...)
	for i := range y.Probes {
		probe := &y.Probes[i]
		if probe.Mode == "" {
			probe.Mode = ProbeModeReadiness
		}
		if probe.Description == "" {
			probe.Description = fmt.Sprintf("user probe %d/%d", i+1, len(y.Probes))
		}
	}

	y.PortForwards = append(append(o.PortForwards, y.PortForwards...), d.PortForwards...)
	instDir := filepath.Dir(filePath)
	for i := range y.PortForwards {
		FillPortForwardDefaults(&y.PortForwards[i], instDir)
		// After defaults processing the singular HostPort and GuestPort values should not be used again.
	}

	if y.SSH.LocalPort == nil {
		y.SSH.LocalPort = d.SSH.LocalPort
	}
	if o.SSH.LocalPort != nil {
		y.SSH.LocalPort = o.SSH.LocalPort
	}
	if y.SSH.LocalPort == nil {
		// y.SSH.LocalPort value is not filled here (filled by the hostagent)
		y.SSH.LocalPort = pointer.Int(0)
	}
	if y.SSH.LoadDotSSHPubKeys == nil {
		y.SSH.LoadDotSSHPubKeys = d.SSH.LoadDotSSHPubKeys
	}
	if o.SSH.LoadDotSSHPubKeys != nil {
		y.SSH.LoadDotSSHPubKeys = o.SSH.LoadDotSSHPubKeys
	}
	if y.SSH.LoadDotSSHPubKeys == nil {
		y.SSH.LoadDotSSHPubKeys = pointer.Bool(true)
	}

	if y.SSH.ForwardAgent == nil {
		y.SSH.ForwardAgent = d.SSH.ForwardAgent
	}
	if o.SSH.ForwardAgent != nil {
		y.SSH.ForwardAgent = o.SSH.ForwardAgent
	}
	if y.SSH.ForwardAgent == nil {
		y.SSH.ForwardAgent = pointer.Bool(false)
	}

	// Combine all mounts; highest priority entry determines writable status.
	// Only works for exact matches; does not normalize case or resolve symlinks.
	mounts := make([]Mount, 0, len(d.Mounts)+len(y.Mounts)+len(o.Mounts))
	location := make(map[string]int)
	for _, mount := range append(append(d.Mounts, y.Mounts...), o.Mounts...) {
		if i, ok := location[mount.Location]; ok {
			if mount.Writable != nil {
				mounts[i].Writable = mount.Writable
			}
		} else {
			location[mount.Location] = len(mounts)
			mounts = append(mounts, mount)
		}
	}
	y.Mounts = mounts

	for i := range y.Mounts {
		mount := &y.Mounts[i]
		if mount.Writable == nil {
			mount.Writable = pointer.Bool(false)
		}
	}
}

func NewArch(arch string) Arch {
	switch arch {
	case "amd64":
		return X8664
	case "arm64":
		return AARCH64
	default:
		logrus.Warnf("Unknown arch: %s", arch)
		return arch
	}
}

func ResolveArch() Arch {
	return NewArch(runtime.GOARCH)
}

func FillPortForwardDefaults(rule *PortForward, instDir string) {
	if rule.Proto == "" {
		rule.Proto = TCP
	}
	if rule.GuestIP == nil {
		if rule.GuestIPMustBeZero {
			rule.GuestIP = net.IPv4zero
		} else {
			rule.GuestIP = api.IPv4loopback1
		}
	}
	if rule.HostIP == nil {
		rule.HostIP = api.IPv4loopback1
	}
	if rule.GuestPortRange[0] == 0 && rule.GuestPortRange[1] == 0 {
		if rule.GuestPort == 0 {
			rule.GuestPortRange[0] = 1
			rule.GuestPortRange[1] = 65535
		} else {
			rule.GuestPortRange[0] = rule.GuestPort
			rule.GuestPortRange[1] = rule.GuestPort
		}
	}
	if rule.HostPortRange[0] == 0 && rule.HostPortRange[1] == 0 {
		if rule.HostPort == 0 {
			rule.HostPortRange = rule.GuestPortRange
		} else {
			rule.HostPortRange[0] = rule.HostPort
			rule.HostPortRange[1] = rule.HostPort
		}
	}
	if rule.GuestSocket != "" {
		tmpl, err := template.New("").Parse(rule.GuestSocket)
		if err == nil {
			user, _ := osutil.MacVZUser(false)
			data := map[string]string{
				"Home": fmt.Sprintf("/home/%s.linux", user.Username),
				"UID":  user.Uid,
				"User": user.Username,
			}
			var out bytes.Buffer
			if err := tmpl.Execute(&out, data); err == nil {
				rule.GuestSocket = out.String()
			} else {
				logrus.WithError(err).Warnf("Couldn't process guestSocket %q as a template", rule.GuestSocket)
			}
		}
	}
	if rule.HostSocket != "" {
		tmpl, err := template.New("").Parse(rule.HostSocket)
		if err == nil {
			user, _ := osuser.Current()
			home, _ := os.UserHomeDir()
			data := map[string]string{
				"Dir":  instDir,
				"Home": home,
				"Name": filepath.Base(instDir),
				"UID":  user.Uid,
				"User": user.Username,
			}
			var out bytes.Buffer
			if err := tmpl.Execute(&out, data); err == nil {
				rule.HostSocket = out.String()
			} else {
				logrus.WithError(err).Warnf("Couldn't process hostSocket %q as a template", rule.HostSocket)
			}
		}
		if !filepath.IsAbs(rule.HostSocket) {
			rule.HostSocket = filepath.Join(instDir, filenames.SocketDir, rule.HostSocket)
		}
	}
}

func Cname(host string) string {
	host = strings.ToLower(host)
	if !strings.HasSuffix(host, ".") {
		host += "."
	}
	return host
}
