package yaml

import "net"

type MacVZYaml struct {
	Images     []Image `yaml:"images" json:"images"` // REQUIRED
	CPUs       *int    `yaml:"cpus,omitempty" json:"cpus,omitempty"`
	Memory     *string `yaml:"memory,omitempty" json:"memory,omitempty"` // go-units.RAMInBytes
	Disk       *string `yaml:"disk,omitempty" json:"disk,omitempty"`     // go-units.RAMInBytes
	Mounts     []Mount `yaml:"mounts,omitempty" json:"mounts,omitempty"`
	MACAddress *string `yaml:"MACAddress,omitempty" json:"MACAddress,omitempty"`

	SSH          SSH           `yaml:"ssh,omitempty" json:"ssh,omitempty"` // REQUIRED
	PortForwards []PortForward `yaml:"portForwards,omitempty" json:"portForwards,omitempty"`
	Provision    []Provision   `yaml:"provision,omitempty" json:"provision,omitempty"`
	Probes       []Probe       `yaml:"probes,omitempty" json:"probes,omitempty"`
	HostResolver HostResolver  `yaml:"hostResolver,omitempty" json:"hostResolver,omitempty"`
}

type Image struct {
	Kernel  string `yaml:"kernel" json:"kernel"`   // REQUIRED
	Initram string `yaml:"initram" json:"initram"` // REQUIRED
	Base    string `yaml:"base" json:"base"`       // REQUIRED
	Arch    Arch   `yaml:"arch,omitempty" json:"arch,omitempty"`
}

type Arch = string

const (
	X8664   Arch = "x86_64"
	AARCH64 Arch = "aarch64"
)

type Mount struct {
	Location string `yaml:"location" json:"location"` // REQUIRED
	Writable *bool  `yaml:"writable,omitempty" json:"writable,omitempty"`
}

type ProvisionMode = string

const (
	ProvisionModeSystem ProvisionMode = "system"
	ProvisionModeUser   ProvisionMode = "user"
)

type Provision struct {
	Mode   ProvisionMode `yaml:"mode" json:"mode"` // default: "system"
	Script string        `yaml:"script" json:"script"`
}

type ProbeMode = string

const (
	ProbeModeReadiness ProbeMode = "readiness"
)

type Probe struct {
	Mode        ProbeMode // default: "readiness"
	Description string
	Script      string
	Hint        string
}

type SSH struct {
	LocalPort *int `yaml:"localPort,omitempty" json:"localPort,omitempty"`

	// LoadDotSSHPubKeys loads ~/.ssh/*.pub in addition to $MACVZ_HOME/_config/user.pub .
	LoadDotSSHPubKeys *bool `yaml:"loadDotSSHPubKeys,omitempty" json:"loadDotSSHPubKeys,omitempty"` // default: true
	ForwardAgent      *bool `yaml:"forwardAgent,omitempty" json:"forwardAgent,omitempty"`           // default: false
}

type Proto = string

const (
	TCP Proto = "tcp"
)

type PortForward struct {
	GuestIPMustBeZero bool   `yaml:"guestIPMustBeZero,omitempty" json:"guestIPMustBeZero,omitempty"`
	GuestIP           net.IP `yaml:"guestIP,omitempty" json:"guestIP,omitempty"`
	GuestPort         int    `yaml:"guestPort,omitempty" json:"guestPort,omitempty"`
	GuestPortRange    [2]int `yaml:"guestPortRange,omitempty" json:"guestPortRange,omitempty"`
	GuestSocket       string `yaml:"guestSocket,omitempty" json:"guestSocket,omitempty"`
	HostIP            net.IP `yaml:"hostIP,omitempty" json:"hostIP,omitempty"`
	HostPort          int    `yaml:"hostPort,omitempty" json:"hostPort,omitempty"`
	HostPortRange     [2]int `yaml:"hostPortRange,omitempty" json:"hostPortRange,omitempty"`
	HostSocket        string `yaml:"hostSocket,omitempty" json:"hostSocket,omitempty"`
	Proto             Proto  `yaml:"proto,omitempty" json:"proto,omitempty"`
	Ignore            bool   `yaml:"ignore,omitempty" json:"ignore,omitempty"`
}

type HostResolver struct {
	IPv6  *bool             `yaml:"ipv6,omitempty" json:"ipv6,omitempty"`
	Hosts map[string]string `yaml:"hosts,omitempty" json:"hosts,omitempty"`
}
