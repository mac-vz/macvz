package cidata

import (
	"errors"
	"fmt"
	"github.com/balaji113/macvz/pkg/sshutil"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/balaji113/macvz/pkg/iso9660util"
	"github.com/balaji113/macvz/pkg/localpathutil"
	"github.com/balaji113/macvz/pkg/osutil"
	"github.com/balaji113/macvz/pkg/store/filenames"
	"github.com/balaji113/macvz/pkg/yaml"
)

func GenerateISO9660(instDir, name string, y *yaml.MacVZYaml) error {
	if err := yaml.Validate(*y, false); err != nil {
		return err
	}
	u, err := osutil.MacVZUser(true)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return err
	}
	args := TemplateArgs{
		Name: name,
		User: u.Username,
		UID:  uid,
	}

	// change instance id on every boot so network config will be processed again
	args.IID = fmt.Sprintf("iid-%d", time.Now().Unix())

	pubKeys, err := sshutil.DefaultPubKeys(true)
	if err != nil {
		return err
	}
	if len(pubKeys) == 0 {
		return errors.New("no SSH key was found, run `ssh-keygen`")
	}
	for _, f := range pubKeys {
		args.SSHPubKeys = append(args.SSHPubKeys, f.Content)
	}

	for _, f := range y.Mounts {
		expanded, err := localpathutil.Expand(f.Location)
		if err != nil {
			return err
		}
		args.Mounts = append(args.Mounts, expanded)
	}

	if err := ValidateTemplateArgs(args); err != nil {
		return err
	}

	layout, err := ExecuteTemplate(args)
	if err != nil {
		return err
	}

	var sb strings.Builder
	for _, mount := range y.Mounts {
		sb.WriteString(fmt.Sprintf("sudo mkdir -p %s\n", mount.Location))
		sb.WriteString(fmt.Sprintf("sudo mount -t virtiofs %s %s", mount.Location, mount.Location))
	}
	layout = append(layout, iso9660util.Entry{
		Path:   fmt.Sprintf("provision.%s/%08d", yaml.ProvisionModeSystem, 0),
		Reader: strings.NewReader(sb.String()),
	})

	for i, f := range y.Provision {
		switch f.Mode {
		case yaml.ProvisionModeSystem, yaml.ProvisionModeUser:
			layout = append(layout, iso9660util.Entry{
				Path:   fmt.Sprintf("provision.%s/%08d", f.Mode, i+1),
				Reader: strings.NewReader(f.Script),
			})
		default:
			return fmt.Errorf("unknown provision mode %q", f.Mode)
		}
	}

	if guestAgentBinary, err := GuestAgentBinary(y.Images[0].Arch); err != nil {
		return err
	} else {
		defer guestAgentBinary.Close()
		layout = append(layout, iso9660util.Entry{
			Path:   "macvz-guestagent",
			Reader: guestAgentBinary,
		})
	}

	return iso9660util.Write(filepath.Join(instDir, filenames.CIDataISO), "cidata", layout)
}

func GuestAgentBinary(arch string) (io.ReadCloser, error) {
	if arch == "" {
		return nil, errors.New("arch must be set")
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	selfSt, err := os.Stat(self)
	if err != nil {
		return nil, err
	}
	if selfSt.Mode()&fs.ModeSymlink != 0 {
		self, err = os.Readlink(self)
		if err != nil {
			return nil, err
		}
	}

	// self:  /usr/local/bin/limactl
	selfDir := filepath.Dir(self)
	selfDirDir := filepath.Dir(selfDir)
	candidates := []string{
		// candidate 0:
		// - self:  /Applications/Lima.app/Contents/MacOS/limactl
		// - agent: /Applications/Lima.app/Contents/MacOS/lima-guestagent.Linux-x86_64
		filepath.Join(selfDir, "macvz-guestagent.Linux-"+arch),
		// candidate 1:
		// - self:  /usr/local/bin/limactl
		// - agent: /usr/local/share/lima/lima-guestagent.Linux-x86_64
		filepath.Join(selfDirDir, "share/macvz/macvz-guestagent.Linux-"+arch),
	}
	for _, candidate := range candidates {
		if f, err := os.Open(candidate); err == nil {
			return f, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("failed to find \"macvz-guestagent.Linux-%s\" binary for %q, attempted %v",
		arch, self, candidates)
}
