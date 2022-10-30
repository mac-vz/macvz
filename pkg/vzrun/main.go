package vzrun

import (
	"context"
	"errors"
	"fmt"
	"github.com/Code-Hex/vz/v2"
	"github.com/docker/go-units"
	"github.com/hashicorp/yamux"
	"github.com/mac-vz/macvz/pkg/socket"
	"github.com/mac-vz/macvz/pkg/store"
	"github.com/mac-vz/macvz/pkg/store/filenames"
	"github.com/mac-vz/macvz/pkg/types"
	"github.com/mac-vz/macvz/pkg/yaml"
	"github.com/mitchellh/go-homedir"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// VM VirtualMachine instance
type VM struct {
	Name        string
	InstanceDir string
	MacVZYaml   *yaml.MacVZYaml
	Handlers    map[types.Kind]func(ctx context.Context, stream *yamux.Stream, event interface{})
	sigintCh    chan os.Signal
}

// InitializeVM Create a virtual machine instance
func InitializeVM(
	instName string,
	handlers map[types.Kind]func(ctx context.Context, stream *yamux.Stream, event interface{}),
	sigintCh chan os.Signal,
) (*VM, error) {
	inst, err := store.Inspect(instName)
	if err != nil {
		return nil, err
	}

	y, err := inst.LoadYAML()
	if err != nil {
		return nil, err
	}

	a := &VM{
		MacVZYaml:   y,
		InstanceDir: inst.Dir,
		Name:        inst.Name,
		Handlers:    handlers,
		sigintCh:    sigintCh,
	}
	return a, nil
}

// Run Starts the VM instance
func (vm VM) Run() error {
	y := vm.MacVZYaml

	kernelCommandLineArguments := []string{
		// Use the first virtio console device as system console.
		"console=hvc0",
		"irqfixup",
		// Stop in the initial ramdisk before attempting to transition to
		// the root file system.
		"root=/dev/vda",
	}

	vmlinuz := filepath.Join(vm.InstanceDir, filenames.Kernel)
	initrd := filepath.Join(vm.InstanceDir, filenames.Initrd)
	diskPath := filepath.Join(vm.InstanceDir, filenames.BaseDisk)
	ciData := filepath.Join(vm.InstanceDir, filenames.CIDataISO)

	bootLoader, err := vz.NewLinuxBootLoader(
		vmlinuz,
		vz.WithCommandLine(strings.Join(kernelCommandLineArguments, " ")),
		vz.WithInitrd(initrd),
	)

	bytes, err := units.RAMInBytes(*y.Memory)
	config, err := vz.NewVirtualMachineConfiguration(
		bootLoader,
		uint(*y.CPUs),
		uint64(bytes),
	)
	//setRawMode(os.Stdin)

	// console
	readFile, _ := os.Create(filepath.Join(vm.InstanceDir, filenames.VZStdoutLog))
	writeFile, _ := os.Create(filepath.Join(vm.InstanceDir, filenames.VZStderrLog))
	serialPortAttachment, err := vz.NewFileHandleSerialPortAttachment(readFile, writeFile)
	consoleConfig, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialPortAttachment)

	readFile1, _ := os.Create(filepath.Join(vm.InstanceDir, "read.sock"))
	writeFile1, _ := os.Create(filepath.Join(vm.InstanceDir, "write.sock"))
	serialPortAttachment1, err := vz.NewFileHandleSerialPortAttachment(readFile1, writeFile1)
	console1Config, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialPortAttachment1)

	config.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{
		consoleConfig,
		console1Config,
	})

	// network
	macAddr, err := net.ParseMAC(*y.MACAddress)

	natAttachment, err := vz.NewNATNetworkDeviceAttachment()
	networkConfig, err := vz.NewVirtioNetworkDeviceConfiguration(natAttachment)
	address, err := vz.NewMACAddress(macAddr)
	networkConfig.SetMACAddress(address)

	config.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{
		networkConfig,
	})
	// entropy
	entropyConfig, err := vz.NewVirtioEntropyDeviceConfiguration()
	config.SetEntropyDevicesVirtualMachineConfiguration([]*vz.VirtioEntropyDeviceConfiguration{
		entropyConfig,
	})

	ciDataIso, err := vz.NewDiskImageStorageDeviceAttachment(ciData, true)
	if err != nil {
		logrus.Fatal(err)
	}
	ciDataConfig, err := vz.NewVirtioBlockDeviceConfiguration(ciDataIso)

	diskImageAttachment, err := vz.NewDiskImageStorageDeviceAttachment(diskPath, false)
	if err != nil {
		logrus.Fatal(err)
	}
	storageDeviceConfig, err := vz.NewVirtioBlockDeviceConfiguration(diskImageAttachment)
	config.SetStorageDevicesVirtualMachineConfiguration([]vz.StorageDeviceConfiguration{
		storageDeviceConfig,
		ciDataConfig,
	})

	// traditional memory balloon device which allows for managing guest memory. (optional)
	configuration, err := vz.NewVirtioTraditionalMemoryBalloonDeviceConfiguration()
	config.SetMemoryBalloonDevicesVirtualMachineConfiguration([]vz.MemoryBalloonDeviceConfiguration{
		configuration,
	})

	// socket device (optional)
	deviceConfiguration, err := vz.NewVirtioSocketDeviceConfiguration()
	config.SetSocketDevicesVirtualMachineConfiguration([]vz.SocketDeviceConfiguration{
		deviceConfiguration,
	})

	mounts := make([]vz.DirectorySharingDeviceConfiguration, len(vm.MacVZYaml.Mounts))
	for i, mount := range y.Mounts {
		expand, _ := homedir.Expand(mount.Location)
		config, _ := vz.NewVirtioFileSystemDeviceConfiguration(expand)
		directory, _ := vz.NewSharedDirectory(expand, !*mount.Writable)
		share, _ := vz.NewSingleDirectoryShare(directory)
		config.SetDirectoryShare(share)
		mounts[i] = config
	}
	config.SetDirectorySharingDevicesVirtualMachineConfiguration(mounts)

	validated, err := config.Validate()
	if !validated || err != nil {
		logrus.Fatal("validation failed", err)
	}

	machine, err := vz.NewVirtualMachine(config)

	errCh := make(chan error, 1)

	machine.Start()

	for {
		select {
		case <-vm.sigintCh:
			result, err := machine.RequestStop()
			if err != nil {
				logrus.Println("request stop error:", err)
				return nil
			}
			logrus.Println("recieved signal", result)
		case newState := <-machine.StateChangedNotify():
			if newState == vz.VirtualMachineStateRunning {
				pidFile := filepath.Join(vm.InstanceDir, filenames.VZPid)
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
				yamuxListener, err := vm.createVSockListener(background)
				if err != nil {
					logrus.Fatal("Failed to create listener", err)
				}
				for _, socketDevice := range machine.SocketDevices() {
					listen, err := socketDevice.Listen(47)
					accept, err := listen.Accept()

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

func (vm VM) createVSockListener(ctx context.Context) (*vz.VirtioSocketListener, error) {
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

				go vm.handleFromGuest(ctx, sess)
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

func (vm VM) handleFromGuest(ctx context.Context, sess *yamux.Session) {
	for {
		c, err := sess.AcceptStream()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				logrus.Warn("unable to accept new incoming yamux streams", "error", err)
			}
			return
		}

		go vm.handleGuestConn(ctx, c)
	}
}

func (vm VM) handleGuestConn(ctx context.Context, c *yamux.Stream) {
	defer c.Close()

	_, dec := socket.GetStreamIO(c)

	var genericMap map[string]interface{}

	socket.Read(dec, &genericMap)
	switch genericMap["kind"] {
	case types.InfoMessage:
		info := types.InfoEvent{}
		socket.ReadMap(genericMap, &info)
		vm.Handlers[types.InfoMessage](ctx, c, info)
	case types.PortMessage:
		event := types.PortEvent{}
		socket.ReadMap(genericMap, &event)
		vm.Handlers[types.PortMessage](ctx, c, event)
	case types.DNSMessage:
		dns := types.DNSEvent{}
		socket.ReadMap(genericMap, &dns)
		vm.Handlers[types.DNSMessage](ctx, c, dns)
	}
}
