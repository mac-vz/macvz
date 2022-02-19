package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/balaji113/macvz/pkg/start"
	"github.com/balaji113/macvz/pkg/store"
	"github.com/balaji113/macvz/pkg/store/filenames"
	"github.com/balaji113/macvz/pkg/yaml"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	yaml2 "gopkg.in/yaml.v2"
)

func newStartCommand() *cobra.Command {
	var startCommand = &cobra.Command{
		Use:               "start NAME|FILE.yaml",
		Short:             fmt.Sprintf("Start a new instance with given configuration or starts a existing instance"),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: startBashComplete,
		RunE:              startAction,
	}

	return startCommand
}

func loadOrCreateInstance(cmd *cobra.Command, args []string) (*store.Instance, error) {
	var arg string
	if len(args) == 0 {
		arg = DefaultInstanceName
	} else {
		arg = args[0]
	}

	var (
		instName string
		yBytes   = yaml.DefaultTemplate
		err      error
	)

	const yBytesLimit = 4 * 1024 * 1024 // 4MiB

	if argSeemsHTTPURL(arg) {
		instName, err = instNameFromURL(arg)
		if err != nil {
			return nil, err
		}
		logrus.Debugf("interpreting argument %q as a http url for instance %q", arg, instName)
		resp, err := http.Get(arg)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		yBytes, err = readAtMaximum(resp.Body, yBytesLimit)
		if err != nil {
			return nil, err
		}
	} else if argSeemsFileURL(arg) {
		instName, err = instNameFromURL(arg)
		if err != nil {
			return nil, err
		}
		logrus.Debugf("interpreting argument %q as a file url for instance %q", arg, instName)
		r, err := os.Open(strings.TrimPrefix(arg, "file://"))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		yBytes, err = readAtMaximum(r, yBytesLimit)
		if err != nil {
			return nil, err
		}
	} else if argSeemsYAMLPath(arg) {
		instName, err = instNameFromYAMLPath(arg)
		if err != nil {
			return nil, err
		}
		logrus.Debugf("interpreting argument %q as a file path for instance %q", arg, instName)
		r, err := os.Open(arg)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		yBytes, err = readAtMaximum(r, yBytesLimit)
		if err != nil {
			return nil, err
		}
	} else {
		instName = arg
		logrus.Debugf("interpreting argument %q as an instance name %q", arg, instName)
		if inst, err := store.Inspect(instName); err == nil {
			logrus.Infof("Using the existing instance %q", instName)

			interfaces, _ := net.Interfaces()
			for _, inter := range interfaces {
				logrus.Println("Interface", inter.HardwareAddr)
				addrs, _ := inter.Addrs()
				logrus.Println("Interface address", addrs)
			}
			return inst, nil
		} else {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
	}
	// create a new instance from the template
	instDir, err := store.InstanceDir(instName)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(instDir); !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("instance %q already exists (%q)", instName, instDir)
	}

	if err != nil {
		return nil, err
	}

	filePath := filepath.Join(instDir, filenames.MacVZYAML)
	y, err := yaml.Load(yBytes, filePath)
	if err != nil {
		return nil, err
	}
	if err := yaml.Validate(*y, true); err != nil {
		rejectedYAML := "lima.REJECTED.yaml"
		if writeErr := os.WriteFile(rejectedYAML, yBytes, 0644); writeErr != nil {
			return nil, fmt.Errorf("the YAML is invalid, attempted to save the buffer as %q but failed: %v: %w", rejectedYAML, writeErr, err)
		}
		return nil, fmt.Errorf("the YAML is invalid, saved the buffer as %q: %w", rejectedYAML, err)
	}
	if err := os.MkdirAll(instDir, 0700); err != nil {
		return nil, err
	}

	bytes, err := yaml2.Marshal(&y)
	if err := os.WriteFile(filePath, bytes, 0644); err != nil {
		return nil, err
	}
	return store.Inspect(instName)
}

func startAction(cmd *cobra.Command, args []string) error {
	inst, err := loadOrCreateInstance(cmd, args)
	if err != nil {
		return err
	}
	if len(inst.Errors) > 0 {
		return fmt.Errorf("errors inspecting instance: %+v", inst.Errors)
	}
	switch inst.Status {
	case store.StatusRunning:
		logrus.Infof("The instance %q is already running. Run `%s` to open the shell.",
			inst.Name, inst.Name)
		// Not an error
		return nil
	case store.StatusStopped:
		// NOP
	default:
		logrus.Warnf("expected status %q, got %q", store.StatusStopped, inst.Status)
	}
	ctx := cmd.Context()
	if err != nil {
		return err
	}
	return start.Start(ctx, inst)
}

func argSeemsHTTPURL(arg string) bool {
	u, err := url.Parse(arg)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return true
}

func argSeemsFileURL(arg string) bool {
	u, err := url.Parse(arg)
	if err != nil {
		return false
	}
	return u.Scheme == "file"
}

func argSeemsYAMLPath(arg string) bool {
	if strings.Contains(arg, "/") {
		return true
	}
	lower := strings.ToLower(arg)
	return strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")
}

func instNameFromURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}
	return instNameFromYAMLPath(path.Base(u.Path))
}

func instNameFromYAMLPath(yamlPath string) (string, error) {
	s := strings.ToLower(filepath.Base(yamlPath))
	s = strings.TrimSuffix(strings.TrimSuffix(s, ".yml"), ".yaml")
	s = strings.ReplaceAll(s, ".", "-")
	return s, nil
}

func startBashComplete(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	instances, _ := bashCompleteInstanceNames(cmd)
	return instances, cobra.ShellCompDirectiveDefault
}

func readAtMaximum(r io.Reader, n int64) ([]byte, error) {
	lr := &io.LimitedReader{
		R: r,
		N: n,
	}
	b, err := io.ReadAll(lr)
	if err != nil {
		if errors.Is(err, io.EOF) && lr.N <= 0 {
			err = fmt.Errorf("exceeded the limit (%d bytes): %w", n, err)
		}
	}
	return b, err
}
