package yaml

import (
	"github.com/balaji113/macvz/pkg/vz-wrapper"
	"github.com/sirupsen/logrus"
	"github.com/xorcare/pointer"
	"runtime"
)

func FillDefault(y, d, o *MacVZYaml, filePath string) {
	defaultArch := pointer.String(ResolveArch())

	y.Images = append(append(o.Images, y.Images...), d.Images...)
	for i := range y.Images {
		img := &y.Images[i]
		if img.Arch == "" {
			img.Arch = *defaultArch
		}
	}

	if y.CPUs == nil {
		y.CPUs = d.CPUs
	}
	if o.CPUs != nil {
		y.CPUs = o.CPUs
	}
	if y.CPUs == nil || *y.CPUs == 0 {
		y.CPUs = pointer.Int(4)
	}

	if y.Memory == nil {
		y.Memory = d.Memory
	}
	if o.Memory != nil {
		y.Memory = o.Memory
	}
	if y.Memory == nil || *y.Memory == "" {
		y.Memory = pointer.String("4GiB")
	}

	if y.Disk == nil {
		y.Disk = d.Disk
	}
	if o.Disk != nil {
		y.Disk = o.Disk
	}
	if y.Disk == nil || *y.Disk == "" {
		y.Disk = pointer.String("100GiB")
	}

	if y.MACAddress == nil || *y.MACAddress == "" {
		y.MACAddress = pointer.String(vz.NewRandomLocallyAdministeredMACAddress().String())
	}
	// Combine all mounts; highest priority entry determines writable status.
	// Only works for exact matches; does not normalize case or resolve symlinks.
	mounts := make([]Mount, 0, len(d.Mounts)+len(y.Mounts)+len(o.Mounts))
	location := make(map[string]int)
	for _, mount := range append(append(d.Mounts, y.Mounts...), o.Mounts...) {
		if i, ok := location[mount.Location]; ok {
			if mount.Writable != nil {
				mounts[i].Writable = mount.Writable
			}
		} else {
			location[mount.Location] = len(mounts)
			mounts = append(mounts, mount)
		}
	}
	y.Mounts = mounts

	for i := range y.Mounts {
		mount := &y.Mounts[i]
		if mount.Writable == nil {
			mount.Writable = pointer.Bool(false)
		}
	}
}

func NewArch(arch string) Arch {
	switch arch {
	case "amd64":
		return X8664
	case "arm64":
		return AARCH64
	default:
		logrus.Warnf("Unknown arch: %s", arch)
		return arch
	}
}

func ResolveArch() Arch {
	return NewArch(runtime.GOARCH)
}
