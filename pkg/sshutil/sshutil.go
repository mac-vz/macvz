package sshutil

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/balaji113/macvz/pkg/osutil"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/balaji113/macvz/pkg/lockutil"
	"github.com/balaji113/macvz/pkg/store/dirnames"
	"github.com/balaji113/macvz/pkg/store/filenames"
	"github.com/coreos/go-semver/semver"
	"github.com/sirupsen/logrus"
)

type PubKey struct {
	Filename string
	Content  string
}

func readPublicKey(f string) (PubKey, error) {
	entry := PubKey{
		Filename: f,
	}
	content, err := os.ReadFile(f)
	if err == nil {
		entry.Content = strings.TrimSpace(string(content))
	} else {
		err = fmt.Errorf("failed to read ssh public key %q: %w", f, err)
	}
	return entry, err
}

// DefaultPubKeys returns the public key from $LIMA_HOME/_config/user.pub.
// The key will be created if it does not yet exist.
//
// When loadDotSSH is true, ~/.ssh/*.pub will be appended to make the VM accessible without specifying
// an identity explicitly.
func DefaultPubKeys(loadDotSSH bool) ([]PubKey, error) {
	// Read $LIMA_HOME/_config/user.pub
	configDir, err := dirnames.MacVZConfigDir()
	if err != nil {
		return nil, err
	}
	_, err = os.Stat(filepath.Join(configDir, filenames.UserPrivateKey))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := os.MkdirAll(configDir, 0700); err != nil {
			return nil, fmt.Errorf("could not create %q directory: %w", configDir, err)
		}
		if err := lockutil.WithDirLock(configDir, func() error {
			keygenCmd := exec.Command("ssh-keygen", "-t", "ed25519", "-q", "-N", "", "-f",
				filepath.Join(configDir, filenames.UserPrivateKey))
			logrus.Debugf("executing %v", keygenCmd.Args)
			if out, err := keygenCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to run %v: %q: %w", keygenCmd.Args, string(out), err)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	entry, err := readPublicKey(filepath.Join(configDir, filenames.UserPublicKey))
	if err != nil {
		return nil, err
	}
	res := []PubKey{entry}

	if !loadDotSSH {
		return res, nil
	}

	// Append all of ~/.ssh/*.pub
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(homeDir, ".ssh/*.pub"))
	if err != nil {
		panic(err) // Only possible error is ErrBadPattern, so this should be unreachable.
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".pub") {
			panic(fmt.Errorf("unexpected ssh public key filename %q", f))
		}
		entry, err := readPublicKey(f)
		if err == nil {
			if !detectValidPublicKey(entry.Content) {
				logrus.Warnf("public key %q doesn't seem to be in ssh format", entry.Filename)
			} else {
				res = append(res, entry)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return res, nil
}

var sshInfo struct {
	sync.Once
	// aesAccelerated is set to true when AES acceleration is available.
	// Available on almost all modern Intel/AMD processors.
	aesAccelerated bool
	// openSSHVersion is set to the version of OpenSSH, or semver.New("0.0.0") if the version cannot be determined.
	openSSHVersion semver.Version
}

// SSHOpts adds the following options to CommonOptions: User, ControlMaster, ControlPath, ControlPersist
func SSHOpts(instDir string, useDotSSH, forwardAgent bool) ([]string, error) {
	controlSock := filepath.Join(instDir, filenames.SSHSock)
	if len(controlSock) >= osutil.UnixPathMax {
		return nil, fmt.Errorf("socket path %q is too long: >= UNIX_PATH_MAX=%d", controlSock, osutil.UnixPathMax)
	}

	u, err := osutil.MacVZUser(false)
	if err != nil {
		return nil, err
	}
	opts, err := CommonOpts(useDotSSH)
	if err != nil {
		return nil, err
	}
	opts = append(opts,
		fmt.Sprintf("User=%s", u.Username), // guest and host have the same username, but we should specify the username explicitly (#85)
		"ControlMaster=auto",
		fmt.Sprintf("ControlPath=\"%s\"", controlSock),
		"ControlPersist=5m",
	)
	if forwardAgent {
		opts = append(opts, "ForwardAgent=yes")
	}
	return opts, nil
}

// SSHArgsFromOpts returns ssh args from opts.
// The result always contains {"-F", "/dev/null} in additon to {"-o", "KEY=VALUE", ...}.
func SSHArgsFromOpts(opts []string) []string {
	args := []string{"-F", "/dev/null"}
	for _, o := range opts {
		args = append(args, "-o", o)
	}
	return args
}

// CommonOpts returns ssh option key-value pairs like {"IdentityFile=/path/to/id_foo"}.
// The result may contain different values with the same key.
//
// The result always contains the IdentityFile option.
// The result never contains the Port option.
func CommonOpts(useDotSSH bool) ([]string, error) {
	configDir, err := dirnames.MacVZConfigDir()
	if err != nil {
		return nil, err
	}
	privateKeyPath := filepath.Join(configDir, filenames.UserPrivateKey)
	_, err = os.Stat(privateKeyPath)
	if err != nil {
		return nil, err
	}
	opts := []string{"IdentityFile=\"" + privateKeyPath + "\""}

	// Append all private keys corresponding to ~/.ssh/*.pub to keep old instances working
	// that had been created before lima started using an internal identity.
	if useDotSSH {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		files, err := filepath.Glob(filepath.Join(homeDir, ".ssh/*.pub"))
		if err != nil {
			panic(err) // Only possible error is ErrBadPattern, so this should be unreachable.
		}
		for _, f := range files {
			if !strings.HasSuffix(f, ".pub") {
				panic(fmt.Errorf("unexpected ssh public key filename %q", f))
			}
			privateKeyPath := strings.TrimSuffix(f, ".pub")
			_, err = os.Stat(privateKeyPath)
			if errors.Is(err, fs.ErrNotExist) {
				// Skip .pub files without a matching private key. This is reasonably common,
				// due to major projects like Vault recommending the ${name}-cert.pub format
				// for SSH certificate files.
				//
				// e.g. https://www.vaultproject.io/docs/secrets/ssh/signed-ssh-certificates
				continue
			}
			if err != nil {
				// Fail on permission-related and other path errors
				return nil, err
			}
			opts = append(opts, "IdentityFile=\""+privateKeyPath+"\"")
		}
	}

	opts = append(opts,
		"StrictHostKeyChecking=no",
		"UserKnownHostsFile=/dev/null",
		"NoHostAuthenticationForLocalhost=yes",
		"GSSAPIAuthentication=no",
		"PreferredAuthentications=publickey",
		"Compression=no",
		"BatchMode=yes",
		"IdentitiesOnly=yes",
	)

	sshInfo.Do(func() {
		sshInfo.aesAccelerated = detectAESAcceleration()
		sshInfo.openSSHVersion = DetectOpenSSHVersion()
	})

	// Only OpenSSH version 8.1 and later support adding ciphers to the front of the default set
	if !sshInfo.openSSHVersion.LessThan(*semver.New("8.1.0")) {
		// By default, `ssh` choose chacha20-poly1305@openssh.com, even when AES accelerator is available.
		// (OpenSSH_8.1p1, macOS 11.6, MacBookPro 2020, Core i7-1068NG7)
		//
		// We prioritize AES algorithms when AES accelerator is available.
		if sshInfo.aesAccelerated {
			logrus.Debugf("AES accelerator seems available, prioritizing aes128-gcm@openssh.com and aes256-gcm@openssh.com")
			//opts = append(opts, "Ciphers=\"^aes128-gcm@openssh.com,aes256-gcm@openssh.com\"")
		} else {
			logrus.Debugf("AES accelerator does not seem available, prioritizing chacha20-poly1305@openssh.com")
			//opts = append(opts, "Ciphers=\"^chacha20-poly1305@openssh.com\"")
		}
	}
	return opts, nil
}

func ParseOpenSSHVersion(version []byte) *semver.Version {
	regex := regexp.MustCompile(`^OpenSSH_(\d+\.\d+)(?:p(\d+))?\b`)
	matches := regex.FindSubmatch(version)
	if len(matches) == 3 {
		if len(matches[2]) == 0 {
			matches[2] = []byte("0")
		}
		return semver.New(fmt.Sprintf("%s.%s", matches[1], matches[2]))
	}
	return &semver.Version{}
}

func DetectOpenSSHVersion() semver.Version {
	var (
		v      semver.Version
		stderr bytes.Buffer
	)
	cmd := exec.Command("ssh", "-V")
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		logrus.Warnf("failed to run %v: stderr=%q", cmd.Args, stderr.String())
	} else {
		v = *ParseOpenSSHVersion(stderr.Bytes())
		logrus.Debugf("OpenSSH version %s detected", v)
	}
	return v
}

// detectValidPublicKey returns whether content represent a public key.
// OpenSSH public key format have the structure of '<algorithm> <key> <comment>'.
// By checking 'algorithm' with signature format identifier in 'key' part,
// this function may report false positive but provide better compatibility.
func detectValidPublicKey(content string) bool {
	if strings.ContainsRune(content, '\n') {
		return false
	}
	var spaced = strings.SplitN(content, " ", 3)
	if len(spaced) < 2 {
		return false
	}
	var algo, base64Key = spaced[0], spaced[1]
	var decodedKey, err = base64.StdEncoding.DecodeString(base64Key)
	if err != nil || len(decodedKey) < 4 {
		return false
	}
	var sigLength = binary.BigEndian.Uint32(decodedKey)
	if uint32(len(decodedKey)) < sigLength {
		return false
	}
	var sigFormat = string(decodedKey[4 : 4+sigLength])
	return algo == sigFormat
}
