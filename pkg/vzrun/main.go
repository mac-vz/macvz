package vzrun

import (
	"context"
	"errors"
	"fmt"
	"github.com/docker/go-units"
	"github.com/mac-vz/macvz/pkg/cidata"
	"github.com/mac-vz/macvz/pkg/downloader"
	"github.com/mac-vz/macvz/pkg/iso9660util"
	"github.com/mac-vz/macvz/pkg/socket"
	"github.com/mac-vz/macvz/pkg/store"
	"github.com/mac-vz/macvz/pkg/store/filenames"
	"github.com/mac-vz/macvz/pkg/yaml"
	"github.com/mac-vz/vz"
	proxy "github.com/balaji113/macvz/pkg/gvisor"
	"github.com/sirupsen/logrus"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Name        string
	InstanceDir string
	MacVZYaml   *yaml.MacVZYaml
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
		errs := make([]error, len(cfg.MacVZYaml.Images))
		for i, f := range cfg.MacVZYaml.Images {
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
			fileName := ""
			if f.Arch == yaml.X8664 {
				fileName = "amd64"
			} else {
				fileName = "arm64"
			}
			err = iso9660util.Extract(BaseDiskZip, "focal-server-cloudimg-"+fileName+".img", baseDisk)
			if err != nil {
				errs[i] = fmt.Errorf("failed to extract base image: %w", err)
			}

			ensuredRequiredImages = true
			break
		}
		if !ensuredRequiredImages {
			return fmt.Errorf("failed to download the required images, attempted %d candidates, errors=%v",
				len(cfg.MacVZYaml.Images), errs)
		}

		inBytes, _ := units.RAMInBytes(*cfg.MacVZYaml.Disk)
		err := os.Truncate(baseDisk, inBytes)
		if err != nil {
			logrus.Println("Error during basedisk initial resize", err.Error())
			return err
		}
	}
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
		MacVZYaml:   y,
		InstanceDir: inst.Dir,
		Name:        inst.Name,
	}
	return a, nil
}

func Run(cfg Config, sigintCh chan os.Signal, startEvents func(ctx context.Context, vsock socket.VsockConnection)) error {
	y := cfg.MacVZYaml

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

	bytes, err := units.RAMInBytes(*y.Memory)
	config := vz.NewVirtualMachineConfiguration(
		bootLoader,
		uint(*y.CPUs),
		uint64(bytes),
	)
	//setRawMode(os.Stdin)

	// console
	readFile, _ := os.Create(filepath.Join(cfg.InstanceDir, filenames.VZStdoutLog))
	writeFile, _ := os.Create(filepath.Join(cfg.InstanceDir, filenames.VZStderrLog))
	serialPortAttachment := vz.NewFileHandleSerialPortAttachment(readFile, writeFile)
	consoleConfig := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialPortAttachment)
	config.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{
		consoleConfig,
	})

	// network
	serverNetSock := filepath.Join(cfg.InstanceDir, filenames.VZNetServer)
	clientNetSock := filepath.Join(cfg.InstanceDir, filenames.VZNetClient)
	vzGVisorSock := filepath.Join(cfg.InstanceDir, filenames.VZGVisorSock)
	gVisorSock := filepath.Join(cfg.InstanceDir, filenames.GVisorSock)
	macAddr, err := net.ParseMAC(*y.MACAddress)

	serverNet := socket.ListenUnixGram(serverNetSock)
	clientNet := socket.DialUnixGram(clientNetSock, serverNetSock)

	fd := socket.GetFdFromConn(clientNet)

	natAttachment := vz.NewFileHandleNetworkDeviceAttachment(fd)
	networkConfig := vz.NewVirtioNetworkDeviceConfiguration(natAttachment)
	networkConfig.SetMacAddress(vz.NewMACAddress(macAddr))

	go func() {
		err3 := os.Remove(gVisorSock)
		if err3 != nil {
			logrus.Fatal("Error while listening to network.sock", err3)
		}
		err3 = os.Remove(vzGVisorSock)
		if err3 != nil {
			logrus.Fatal("Error while listening to vznetwork.sock", err3)
		}
		proxy.StartProxy(gVisorSock, true, vzGVisorSock, *y.MACAddress, serverNet, clientNet.LocalAddr())
	}()

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

	mounts := make([]vz.DirectorySharingDeviceConfiguration, len(cfg.MacVZYaml.Mounts))
	for i, mount := range y.Mounts {
		mounts[i] = vz.NewVZVirtioFileSystemDeviceConfiguration(mount.Location, mount.Location, !*mount.Writable)
	}
	config.SetDirectorySharingDevices(mounts)

	validated, err := config.Validate()
	if !validated || err != nil {
		logrus.Fatal("validation failed", err)
	}

	vm := vz.NewVirtualMachine(config)

	errCh := make(chan error, 1)

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

				//VM Write and Host reads
				listener := vz.NewVirtioSocketListener(func(conn *vz.VirtioSocketConnection, err error) {
					logrus.Println("Connected")
					background, cancel := context.WithCancel(context.Background())
					defer cancel()
					if err != nil {
						logrus.Error("Error while starting host agent")
					}
					startEvents(background, socket.VsockConnection{Conn: conn})
				})
				for _, socketDevice := range vm.SocketDevices() {
					socketDevice.SetSocketListenerForPort(listener, 2222)
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
