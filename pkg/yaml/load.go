package yaml

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/mac-vz/macvz/pkg/store/dirnames"
	"github.com/mac-vz/macvz/pkg/store/filenames"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// Load loads the yaml and fulfills unspecified fields with the default values.
//
// Load does not validate. Use Validate for validation.
func Load(b []byte, filePath string) (*MacVZYaml, error) {
	var y, d, o MacVZYaml

	if err := yaml.Unmarshal(b, &y); err != nil {
		return nil, err
	}
	configDir, err := dirnames.MacVZConfigDir()
	if err != nil {
		return nil, err
	}

	defaultPath := filepath.Join(configDir, filenames.Default)
	bytes, err := os.ReadFile(defaultPath)
	if err == nil {
		logrus.Debugf("Mixing %q into %q", defaultPath, filePath)
		if err := yaml.Unmarshal(bytes, &d); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	overridePath := filepath.Join(configDir, filenames.Override)
	bytes, err = os.ReadFile(overridePath)
	if err == nil {
		logrus.Debugf("Mixing %q into %q", overridePath, filePath)
		if err := yaml.Unmarshal(bytes, &o); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	FillDefault(&y, &d, &o, filePath)
	return &y, nil
}
