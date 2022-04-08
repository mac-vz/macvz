package cidata

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"github.com/mac-vz/macvz/pkg/iso9660util"
	"io/fs"

	"github.com/mac-vz/macvz/pkg/templateutil"
)

//go:embed cidata.TEMPLATE.d
var templateFS embed.FS

const templateFSRoot = "cidata.TEMPLATE.d"

type Containerd struct {
	System bool
	User   bool
}
type Network struct {
	MACAddress string
	Interface  string
}
type TemplateArgs struct {
	Name            string // instance name
	IID             string // instance id
	User            string // user name
	UID             int
	SSHPubKeys      []string
	Containerd      Containerd
	Networks        []Network
	SlirpNICName    string
	SlirpGateway    string
	SlirpDNS        string
	SlirpIPAddress  string
	UDPDNSLocalPort int
	TCPDNSLocalPort int
	Env             map[string]string
	Hosts           map[string]string
	DNSAddresses    []string
}

func ValidateTemplateArgs(args TemplateArgs) error {
	if args.User == "root" {
		return errors.New("field User must not be \"root\"")
	}
	if args.UID == 0 {
		return errors.New("field UID must not be 0")
	}
	if len(args.SSHPubKeys) == 0 {
		return errors.New("field SSHPubKeys must be set")
	}
	return nil
}

func ExecuteTemplate(args TemplateArgs) ([]iso9660util.Entry, error) {
	if err := ValidateTemplateArgs(args); err != nil {
		return nil, err
	}

	fsys, err := fs.Sub(templateFS, templateFSRoot)
	if err != nil {
		return nil, err
	}

	var layout []iso9660util.Entry
	walkFn := func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("got non-regular file %q", path)
		}
		templateB, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		b, err := templateutil.Execute(string(templateB), args)
		if err != nil {
			return err
		}
		layout = append(layout, iso9660util.Entry{
			Path:   path,
			Reader: bytes.NewReader(b),
		})
		return nil
	}

	if err := fs.WalkDir(fsys, ".", walkFn); err != nil {
		return nil, err
	}

	return layout, nil
}
