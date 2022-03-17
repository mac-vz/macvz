// Package filenames defines the names of the files that appear under an instance dir
// or inside the config directory.
//
// See docs/internal.md .
package filenames

// Instance names starting with an underscore are reserved for lima internal usage

const (
	ConfigDir   = "_config"
	CacheDir    = "_cache"    // not yet implemented
	NetworksDir = "_networks" // network log files are stored here
)

// Filenames used inside the ConfigDir

const (
	UserPrivateKey = "user"
	UserPublicKey  = UserPrivateKey + ".pub"
	NetworksConfig = "networks.yaml"
	Default        = "default.yaml"
	Override       = "override.yaml"
)

// Filenames that may appear under an instance directory

const (
	MacVZYAML   = "macvz.yaml"
	CIDataISO   = "cidata.iso"
	BaseDiskZip = "basedisk.zip"
	BaseDisk    = "basedisk"
	Kernel      = "vmlinux"
	Initrd      = "initrd"
	VZPid       = "macvz.pid"
	VZStdoutLog = "vz.stdout.log"
	VZStderrLog = "vz.stderr.log"

	VZNetServer  = "unixgram-server.sock"
	VZNetClient  = "unixgram-client.sock"
	GVisorSock   = "network.sock"
	VZGVisorSock = "vzgvisor.sock"
)
