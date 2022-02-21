package vzrun

import (
	"context"
	"errors"
	"fmt"
	"github.com/balaji113/macvz/pkg/cidata"
	"github.com/balaji113/macvz/pkg/downloader"
	"github.com/balaji113/macvz/pkg/iso9660util"
	"github.com/balaji113/macvz/pkg/store"
	"github.com/balaji113/macvz/pkg/store/filenames"
	"github.com/balaji113/macvz/pkg/vz-wrapper"
	"github.com/balaji113/macvz/pkg/yaml"
	"github.com/docker/go-units"
	"github.com/sirupsen/logrus"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Name        string
	InstanceDir string
	LimaYAML    *yaml.MacVZYaml
}

func downloadImage(disk string, remote string) error {
	res, err := downloader.Download(disk, remote,
		downloader.WithCache(),
	)
	if err != nil {
		return fmt.Errorf("failed to download %q: %w", remote, err)
	}
	switch res.Status {
	case downloader.StatusDownloaded:
		logrus.Infof("Downloaded image from %q", remote)
	case downloader.StatusUsedCache:
		logrus.Infof("Using cache %q", res.CachePath)
	default:
		logrus.Warnf("Unexpected result from downloader.Download(): %+v", res)
	}
	return nil
}

func EnsureDisk(ctx context.Context, cfg Config) error {
	kernel := filepath.Join(cfg.InstanceDir, filenames.Kernel)
	initrd := filepath.Join(cfg.InstanceDir, filenames.Initrd)
	baseDisk := filepath.Join(cfg.InstanceDir, filenames.BaseDisk)
	BaseDiskZip := filepath.Join(cfg.InstanceDir, filenames.BaseDiskZip)

	if _, err := os.Stat(baseDisk); errors.Is(err, os.ErrNotExist) {
		var ensuredRequiredImages bool
		errs := make([]error, len(cfg.LimaYAML.Images))
		for i, f := range cfg.LimaYAML.Images {
			err := downloadImage(kernel, f.Kernel)
			if err != nil {
				errs[i] = fmt.Errorf("failed to download required images: %w", err)
				continue
			}
			err = downloadImage(initrd, f.Initram)
			if err != nil {
				errs[i] = fmt.Errorf("failed to download required images: %w", err)
				continue
			}
			err = downloadImage(BaseDiskZip, f.Base)
			if err != nil {
				errs[i] = fmt.Errorf("failed to download required images: %w", err)
				continue
			}
			err = iso9660util.Extract(BaseDiskZip, "focal-server-cloudimg-amd64.img", baseDisk)
			if err != nil {
				errs[i] = fmt.Errorf("failed to extract base image: %w", err)
			}

			ensuredRequiredImages = true
			break
		}
		if !ensuredRequiredImages {
			return fmt.Errorf("failed to download the required images, attempted %d candidates, errors=%v",
				len(cfg.LimaYAML.Images), errs)
		}
	}

	bytes, _ := units.RAMInBytes(*cfg.LimaYAML.Disk)
	logrus.Println("Bytes", bytes)
	command := exec.CommandContext(ctx, "/bin/dd", "if=/dev/null", "of="+baseDisk, "bs=1", "count=0",
		"seek="+strconv.FormatInt(bytes, 10))
	err := command.Run()

	if err != nil {
		logrus.Println("Error during resize", err.Error())
		return err
	}
	logrus.Println("Resized")

	return nil
}

func Initialize(instName string) (*Config, error) {
	inst, err := store.Inspect(instName)
	if err != nil {
		return nil, err
	}

	y, err := inst.LoadYAML()
	if err != nil {
		return nil, err
	}

	if err := cidata.GenerateISO9660(inst.Dir, instName, y); err != nil {
		return nil, err
	}
	a := &Config{
		LimaYAML:    y,
		InstanceDir: inst.Dir,
		Name:        inst.Name,
	}
	return a, nil
}

func Run(cfg Config) error {
	y := cfg.LimaYAML

	kernelCommandLineArguments := []string{
		// Use the first virtio console device as system console.
		"console=hvc0",
		"irqfixup",
		// Stop in the initial ramdisk before attempting to transition to
		// the root file system.
		"root=/dev/vda",
	}

	vmlinuz := filepath.Join(cfg.InstanceDir, filenames.Kernel)
	initrd := filepath.Join(cfg.InstanceDir, filenames.Initrd)
	diskPath := filepath.Join(cfg.InstanceDir, filenames.BaseDisk)
	ciData := filepath.Join(cfg.InstanceDir, filenames.CIDataISO)

	bootLoader := vz.NewLinuxBootLoader(
		vmlinuz,
		vz.WithCommandLine(strings.Join(kernelCommandLineArguments, " ")),
		vz.WithInitrd(initrd),
	)
	logrus.Println("bootLoader:", bootLoader)
	logrus.Println("disk:", diskPath)

	bytes, err := units.RAMInBytes(*y.Memory)
	config := vz.NewVirtualMachineConfiguration(
		bootLoader,
		uint(*y.CPUs),
		uint64(bytes),
	)
	//setRawMode(os.Stdin)

	// console
	serialPortAttachment := vz.NewFileHandleSerialPortAttachment(os.Stdin, os.Stdout)
	consoleConfig := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialPortAttachment)
	config.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{
		consoleConfig,
	})

	// network
	natAttachment := vz.NewNATNetworkDeviceAttachment()
	networkConfig := vz.NewVirtioNetworkDeviceConfiguration(natAttachment)
	config.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{
		networkConfig,
	})

	first, err := net.ParseMAC(*y.MACAddress)
	networkConfig.SetMacAddress(vz.NewMACAddress(first))

	// entropy
	entropyConfig := vz.NewVirtioEntropyDeviceConfiguration()
	config.SetEntropyDevicesVirtualMachineConfiguration([]*vz.VirtioEntropyDeviceConfiguration{
		entropyConfig,
	})

	ciDataIso, err := vz.NewDiskImageStorageDeviceAttachment(ciData, true)
	if err != nil {
		logrus.Fatal(err)
	}
	ciDataConfig := vz.NewVirtioBlockDeviceConfiguration(ciDataIso)

	diskImageAttachment, err := vz.NewDiskImageStorageDeviceAttachment(diskPath, false)
	if err != nil {
		logrus.Fatal(err)
	}
	storageDeviceConfig := vz.NewVirtioBlockDeviceConfiguration(diskImageAttachment)
	config.SetStorageDevicesVirtualMachineConfiguration([]vz.StorageDeviceConfiguration{
		storageDeviceConfig,
		ciDataConfig,
	})

	// traditional memory balloon device which allows for managing guest memory. (optional)
	config.SetMemoryBalloonDevicesVirtualMachineConfiguration([]vz.MemoryBalloonDeviceConfiguration{
		vz.NewVirtioTraditionalMemoryBalloonDeviceConfiguration(),
	})

	// socket device (optional)
	config.SetSocketDevicesVirtualMachineConfiguration([]vz.SocketDeviceConfiguration{
		vz.NewVirtioSocketDeviceConfiguration(),
	})

	mounts := make([]vz.DirectorySharingDeviceConfiguration, len(cfg.LimaYAML.Mounts))
	for i, mount := range y.Mounts {
		mounts[i] = vz.NewVZVirtioFileSystemDeviceConfiguration(mount.Location, mount.Location, !*mount.Writable)
	}
	config.SetDirectorySharingDevices(mounts)

	validated, err := config.Validate()
	if !validated || err != nil {
		logrus.Fatal("validation failed", err)
	}

	vm := vz.NewVirtualMachine(config)

	sigintCh := make(chan os.Signal, 1)
	signal.Notify(sigintCh, os.Interrupt, os.Kill)

	errCh := make(chan error, 1)

	logrus.Println("=====VM State=====", vm.State())

	vm.Start(func(err error) {
		if err != nil {
			errCh <- err
		}
	})

	for {
		select {
		case <-sigintCh:
			result, err := vm.RequestStop()
			if err != nil {
				logrus.Println("request stop error:", err)
				return nil
			}
			logrus.Println("recieved signal", result)
		case newState := <-vm.StateChangedNotify():
			if newState == vz.VirtualMachineStateRunning {
				pidFile := filepath.Join(cfg.InstanceDir, filenames.VZPid)
				if err != nil {
					return err
				}
				if pidFile != "" {
					if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
						return fmt.Errorf("pidfile %q already exists", pidFile)
					}
					if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644); err != nil {
						return err
					}
					defer os.RemoveAll(pidFile)
				}
				logrus.Println("start VM is running")
			}
			if newState == vz.VirtualMachineStateStopped {
				logrus.Println("stopped successfully")
				return nil
			}
		case err := <-errCh:
			logrus.Println("in start:", err)
		}
	}
}
