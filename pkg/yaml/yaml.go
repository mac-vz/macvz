package yaml

type MacVZYaml struct {
	Images     []Image `yaml:"images" json:"images"` // REQUIRED
	CPUs       *int    `yaml:"cpus,omitempty" json:"cpus,omitempty"`
	Memory     *string `yaml:"memory,omitempty" json:"memory,omitempty"` // go-units.RAMInBytes
	Disk       *string `yaml:"disk,omitempty" json:"disk,omitempty"`     // go-units.RAMInBytes
	Mounts     []Mount `yaml:"mounts,omitempty" json:"mounts,omitempty"`
	MACAddress *string `yaml:"MACAddress,omitempty" json:"MACAddress,omitempty"`
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
