package store

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mac-vz/macvz/pkg/store/dirnames"
	"github.com/mac-vz/macvz/pkg/yaml"
)

// Instances returns the names of the instances under MacVZDir.
func Instances() ([]string, error) {
	limaDir, err := dirnames.MacVZDir()
	if err != nil {
		return nil, err
	}
	limaDirList, err := os.ReadDir(limaDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, f := range limaDirList {
		if strings.HasPrefix(f.Name(), ".") || strings.HasPrefix(f.Name(), "_") {
			continue
		}
		names = append(names, f.Name())
	}
	return names, nil
}

// InstanceDir returns the instance dir.
// InstanceDir does not check whether the instance exists
func InstanceDir(name string) (string, error) {
	limaDir, err := dirnames.MacVZDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(limaDir, name)
	return dir, nil
}

// LoadYAMLByFilePath loads and validates the yaml.
func LoadYAMLByFilePath(filePath string) (*yaml.MacVZYaml, error) {
	// We need to use the absolute path because it may be used to determine hostSocket locations.
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}
	yContent, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	y, err := yaml.Load(yContent, absPath)
	if err != nil {
		return nil, err
	}
	if err := yaml.Validate(*y, false); err != nil {
		return nil, err
	}
	return y, nil
}
