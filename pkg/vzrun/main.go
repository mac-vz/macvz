package vzrun

import (
	"context"
	"errors"
	"fmt"
	"github.com/docker/go-units"
	"github.com/fxamacker/cbor/v2"
	"github.com/hashicorp/yamux"
	"github.com/mac-vz/macvz/pkg/downloader"
	"github.com/mac-vz/macvz/pkg/iso9660util"
	"github.com/mac-vz/macvz/pkg/store"
	"github.com/mac-vz/macvz/pkg/store/filenames"
	"github.com/mac-vz/macvz/pkg/types"
	"github.com/mac-vz/macvz/pkg/yaml"
	"github.com/mac-vz/vz"
	"github.com/mitchellh/mapstructure"
	"github.com/sirupsen/logrus"
	"io"
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

	a := &Config{
		MacVZYaml:   y,
		InstanceDir: inst.Dir,
		Name:        inst.Name,
	}
	return a, nil
}

func Run(cfg Config, sigintCh chan os.Signal, handlePortEvent func(ctx context.Context, portEvent types.PortEvent)) error {
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

	readFile1, _ := os.Create(filepath.Join(cfg.InstanceDir, "read.sock"))
	writeFile1, _ := os.Create(filepath.Join(cfg.InstanceDir, "write.sock"))
	serialPortAttachment1 := vz.NewFileHandleSerialPortAttachment(readFile1, writeFile1)
	console1Config := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialPortAttachment1)

	config.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{
		consoleConfig,
		console1Config,
	})

	// network
	macAddr, err := net.ParseMAC(*y.MACAddress)

	natAttachment := vz.NewNATNetworkDeviceAttachment()
	networkConfig := vz.NewVirtioNetworkDeviceConfiguration(natAttachment)
	networkConfig.SetMACAddress(vz.NewMACAddress(macAddr))

	config.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{
		networkConfig,
	})
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

				background, cancel := context.WithCancel(context.Background())
				defer cancel()
				yamuxListener, err := createVSockListener(background, handlePortEvent)
				if err != nil {
					logrus.Fatal("Failed to create listener", err)
				}
				for _, socketDevice := range vm.SocketDevices() {
					socketDevice.SetSocketListenerForPort(yamuxListener, 47)
				}
				logrus.Println("start VM is running")
			}
			if newState == vz.VirtualMachineStateStopped {
				logrus.Println("stopped successfully")
				return nil
			}
		case err := <-errCh:
			logrus.Println("in start:", err)
			return errors.New("error during start of VM")
		}
	}
}

func createVSockListener(ctx context.Context, handlePortEvent func(ctx context.Context, portEvent types.PortEvent)) (*vz.VirtioSocketListener, error) {

	connCh := make(chan *vz.VirtioSocketConnection)

	go func() {
		var (
			conn *vz.VirtioSocketConnection
			sess *yamux.Session
		)

		for {
			select {
			case ci := <-connCh:
				conn = ci

				if sess != nil {
					sess.Close()
				}

				cfg := yamux.DefaultConfig()
				cfg.EnableKeepAlive = true
				cfg.AcceptBacklog = 10

				sess, _ = yamux.Client(conn, cfg)

				go handleFromGuest(ctx, sess, handlePortEvent)
			}
		}
	}()

	listener := vz.NewVirtioSocketListener(func(conn *vz.VirtioSocketConnection, err error) {
		if err != nil {
			return
		}
		connCh <- conn
	})

	return listener, nil
}

func handleFromGuest(ctx context.Context, sess *yamux.Session, handlePortEvent func(ctx context.Context, portEvent types.PortEvent)) {
	for {
		c, err := sess.AcceptStream()
		logrus.Info("Found new connection")
		if err != nil {
			if !errors.Is(err, io.EOF) {
				logrus.Warn("unable to accept new incoming yamux streams", "error", err)
			}
			return
		}

		go handleGuestConn(ctx, c, handlePortEvent)
	}
}

func handleGuestConn(ctx context.Context, c *yamux.Stream, handlePortEvent func(ctx context.Context, portEvent types.PortEvent)) {
	defer c.Close()

	dec := cbor.NewDecoder(c)

	var genericMap map[string]interface{}

	err := dec.Decode(&genericMap)
	logrus.Info("Decoded Map", genericMap)
	if err != nil {
		logrus.Error("error decoding response", err)
	}

	switch genericMap["kind"] {
	case types.InfoMessage:
		info := types.InfoEvent{}
		decode(genericMap, &info)
		logrus.Info("Event received", info)
	case types.PortMessage:
		event := types.PortEvent{}
		decode(genericMap, &event)
		logrus.Info("Event received", event)
		handlePortEvent(context.Background(), event)
	}
}

func encode(enc *cbor.Encoder, message interface{}) {
	err := enc.Encode(message)
	if err != nil {
		logrus.Error("error encoding response", err)
	}
}

func decode(src map[string]interface{}, dest interface{}) {
	err := mapstructure.Decode(src, dest)
	if err != nil {
		logrus.Error("error mapping decoded values", err)
	}
}
