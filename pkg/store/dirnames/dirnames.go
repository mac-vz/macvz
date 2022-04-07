package dirnames

import (
	"errors"
	"fmt"
	"github.com/mac-vz/macvz/pkg/store/filenames"
	"os"
	"path/filepath"
)

// DotLima is a directory that appears under the home directory.
const DotLima = ".macvz"

// MacVZDir returns the abstract path of `~/.lima` (or $LIMA_HOME, if set).
//
// NOTE: We do not use `~/Library/Application Support/Lima` on macOS.
// We use `~/.lima` so that we can have enough space for the length of the socket path,
// which can be only 104 characters on macOS.
func MacVZDir() (string, error) {
	dir := os.Getenv("MACVZ_HOME")
	if dir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(homeDir, DotLima)
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return dir, nil
	}
	realdir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("cannot evaluate symlinks in %q: %w", dir, err)
	}
	return realdir, nil
}

// LimaConfigDir returns the path of the config directory, $MACVZ_HOME/_config.
func MacVZConfigDir() (string, error) {
	limaDir, err := MacVZDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(limaDir, filenames.ConfigDir), nil
}
