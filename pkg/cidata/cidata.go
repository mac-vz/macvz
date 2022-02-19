package cidata

import (
	"errors"
	"fmt"
	"github.com/balaji113/macvz/pkg/sshutil"
	"path/filepath"
	"strconv"
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

	return iso9660util.Write(filepath.Join(instDir, filenames.CIDataISO), "cidata", layout)
}
