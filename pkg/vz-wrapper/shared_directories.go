package vz

/*
#cgo darwin CFLAGS: -x objective-c -fno-objc-arc
#cgo darwin LDFLAGS: -lobjc -framework Foundation -framework Virtualization
# include "virtualization.h"
*/
import "C"
import "runtime"

type DirectorySharingDeviceConfiguration interface {
	NSObject

	directorySharingDeviceConfiguration()
}

type baseDirectorySharingDeviceConfiguration struct{}

func (*baseDirectorySharingDeviceConfiguration) directorySharingDeviceConfiguration() {}

var _ DirectorySharingDeviceConfiguration = (*VZVirtioFileSystemDeviceConfiguration)(nil)

type VZVirtioFileSystemDeviceConfiguration struct {
	pointer

	*baseDirectorySharingDeviceConfiguration
}

func MountFolder(tagName string, path string) {

}

func NewVZVirtioFileSystemDeviceConfiguration(tagName string, folder string, readOnly bool) *VZVirtioFileSystemDeviceConfiguration {
	tagNameChars := charWithGoString(tagName)
	defer tagNameChars.Free()

	folderChars := charWithGoString(folder)
	defer folderChars.Free()

	config := &VZVirtioFileSystemDeviceConfiguration{
		pointer: pointer{
			ptr: C.newVZVirtioDirectorySharingDeviceConfiguration(tagNameChars.CString(),
				folderChars.CString(), C.bool(readOnly)),
		},
	}
	runtime.SetFinalizer(config, func(self *VZVirtioFileSystemDeviceConfiguration) {
		self.Release()
	})
	return config
}
