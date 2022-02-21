package cidata

import (
	"errors"
	"fmt"
	"github.com/balaji113/macvz/pkg/sshutil"
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

	return iso9660util.Write(filepath.Join(instDir, filenames.CIDataISO), "cidata", layout)
}
