package hostagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lima-vm/sshocker/pkg/ssh"
	"github.com/mac-vz/macvz/pkg/cidata"
	"github.com/mac-vz/macvz/pkg/hostagent/dns"
	"github.com/mac-vz/macvz/pkg/hostagent/events"
	"github.com/mac-vz/macvz/pkg/socket"
	"github.com/mac-vz/macvz/pkg/vzrun"
	"github.com/mac-vz/macvz/pkg/yaml"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	guestagentapi "github.com/mac-vz/macvz/pkg/guestagent/api"
	"github.com/mac-vz/macvz/pkg/sshutil"
	"github.com/mac-vz/macvz/pkg/store"
	"github.com/sirupsen/logrus"
)

type HostAgent struct {
	y             *yaml.MacVZYaml
	instDir       string
	instName      string
	sshConfig     *ssh.SSHConfig
	portForwarder *portForwarder

	udpDNSLocalPort int
	tcpDNSLocalPort int

	onClose []func() error // LIFO

	sigintCh chan os.Signal

	vsock      socket.VsockConnection
	sshRemote  string
	eventEnc   *json.Encoder
	eventEncMu sync.Mutex
}

// New creates the HostAgent.
//
// stdout is for emitting JSON lines of Events.
func New(instName string, sigintCh chan os.Signal) (*HostAgent, error) {
	inst, err := store.Inspect(instName)
	if err != nil {
		return nil, err
	}

	y, err := inst.LoadYAML()
	if err != nil {
		return nil, err
	}
	// y is loaded with FillDefault() already, so no need to care about nil pointers.

	sshOpts, err := sshutil.SSHOpts(inst.Dir, *y.SSH.LoadDotSSHPubKeys, *y.SSH.ForwardAgent)
	if err != nil {
		return nil, err
	}
	sshConfig := &ssh.SSHConfig{
		AdditionalArgs: sshutil.SSHArgsFromOpts(sshOpts),
	}

	var udpDNSLocalPort, tcpDNSLocalPort int
	if *y.HostResolver.Enabled {
		udpDNSLocalPort, err = findFreeUDPLocalPort()
		if err != nil {
			return nil, err
		}
		tcpDNSLocalPort, err = findFreeTCPLocalPort()
		if err != nil {
			return nil, err
		}
	}

	if err := cidata.GenerateISO9660(inst.Dir, instName, y, udpDNSLocalPort, tcpDNSLocalPort); err != nil {
		return nil, err
	}

	rules := make([]yaml.PortForward, 0, 2+len(y.PortForwards))
	// Block ports 22 and sshLocalPort on all IPs
	for _, port := range []int{22} {
		rule := yaml.PortForward{GuestIP: net.IPv4zero, GuestPort: port, Ignore: true}
		yaml.FillPortForwardDefaults(&rule, inst.Dir)
		rules = append(rules, rule)
	}
	rules = append(rules, y.PortForwards...)
	// Default forwards for all non-privileged ports from "127.0.0.1" and "::1"
	rule := yaml.PortForward{GuestIP: guestagentapi.IPv4loopback1}
	yaml.FillPortForwardDefaults(&rule, inst.Dir)
	rules = append(rules, rule)

	a := &HostAgent{
		y:               y,
		instDir:         inst.Dir,
		instName:        instName,
		sshConfig:       sshConfig,
		udpDNSLocalPort: udpDNSLocalPort,
		tcpDNSLocalPort: tcpDNSLocalPort,
		sigintCh:        sigintCh,
		eventEnc:        json.NewEncoder(os.Stdout),
		portForwarder:   newPortForwarder(sshConfig, rules),
	}

	return a, nil
}

func findFreeTCPLocalPort() (int, error) {
	lAddr0, err := net.ResolveTCPAddr("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp4", lAddr0)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	lAddr := l.Addr()
	lTCPAddr, ok := lAddr.(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("expected *net.TCPAddr, got %v", lAddr)
	}
	port := lTCPAddr.Port
	if port <= 0 {
		return 0, fmt.Errorf("unexpected port %d", port)
	}
	return port, nil
}

func findFreeUDPLocalPort() (int, error) {
	lAddr0, err := net.ResolveUDPAddr("udp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenUDP("udp4", lAddr0)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	lAddr := l.LocalAddr()
	lUDPAddr, ok := lAddr.(*net.UDPAddr)
	if !ok {
		return 0, fmt.Errorf("expected *net.UDPAddr, got %v", lAddr)
	}
	port := lUDPAddr.Port
	if port <= 0 {
		return 0, fmt.Errorf("unexpected port %d", port)
	}
	return port, nil
}

func (a *HostAgent) Run(ctx context.Context) error {
	defer func() {
		exitingEv := events.Event{
			Status: events.Status{
				Exiting: true,
			},
		}
		a.emitEvent(ctx, exitingEv)
	}()

	if *a.y.HostResolver.Enabled {
		hosts := a.y.HostResolver.Hosts
		hosts["host.macvz.internal."] = "192.168.106.1"
		hosts[fmt.Sprintf("macvz-%s.", a.instName)] = "192.168.106.1"
		dnsServer, err := dns.Start(a.udpDNSLocalPort, a.tcpDNSLocalPort, *a.y.HostResolver.IPv6, hosts)
		logrus.Println("======DNS Server started=======")
		if err != nil {
			return fmt.Errorf("cannot start DNS server: %w", err)
		}
		defer dnsServer.Shutdown()
	}

	stBooting := events.Status{}
	a.emitEvent(ctx, events.Event{Status: stBooting})

	ctxHA, cancelHA := context.WithCancel(ctx)
	go func() {
		stRunning := events.Status{}
		if haErr := a.startHostAgentRoutines(ctxHA); haErr != nil {
			stRunning.Degraded = true
			stRunning.Errors = append(stRunning.Errors, haErr.Error())
		}
		stRunning.Running = true
		a.emitEvent(ctx, events.Event{Status: stRunning})
	}()

	//Init vm
	initialize, err := vzrun.Initialize(a.instName)
	if err != nil {
		logrus.Fatal("INIT", err)
	}
	err = vzrun.Run(*initialize, a.sigintCh, func(ctx context.Context, vsock socket.VsockConnection) {
		a.WatchGuestAgentEvents(ctx, vsock)
	})
	if err != nil {
		logrus.Fatal("RUN", err)
	}
	cancelHA()
	return nil
}

func (a *HostAgent) setSSHRemote(remote string) {
	a.sshRemote = remote
}

func (a *HostAgent) startHostAgentRoutines(ctx context.Context) error {
	a.onClose = append(a.onClose, func() error {
		logrus.Debugf("shutting down the SSH master")
		if exitMasterErr := ssh.ExitMaster(a.sshRemote, 22, a.sshConfig); exitMasterErr != nil {
			logrus.WithError(exitMasterErr).Warn("failed to exit SSH master")
		}
		return nil
	})

	var mErr error
	if err := a.waitForRequirements(ctx, "host", a.hostRequirements()); err != nil {
		mErr = multierror.Append(mErr, err)
	}
	sshRemoteUser := sshutil.SSHRemoteUser(*a.y.MACAddress)
	a.setSSHRemote(sshRemoteUser)

	if err := a.waitForRequirements(ctx, "essential", a.essentialRequirements()); err != nil {
		mErr = multierror.Append(mErr, err)
	}
	if err := a.waitForRequirements(ctx, "optional", a.optionalRequirements()); err != nil {
		mErr = multierror.Append(mErr, err)
	}
	go a.ForwardDefinedSockets(ctx)
	if err := a.waitForRequirements(ctx, "final", a.finalRequirements()); err != nil {
		mErr = multierror.Append(mErr, err)
	}
	return mErr
}

func (a *HostAgent) ForwardDefinedSockets(ctx context.Context) {
	// Setup all socket forwards and defer their teardown
	logrus.Debugf("Forwarding unix sockets")
	for _, rule := range a.y.PortForwards {
		if rule.GuestSocket != "" {
			local := hostAddress(rule, guestagentapi.IPPort{})
			_ = forwardSSH(ctx, a.sshConfig, a.sshRemote, local, rule.GuestSocket, verbForward)
		}
	}

	a.onClose = append(a.onClose, func() error {
		logrus.Debugf("Stop forwarding unix sockets")
		var mErr error
		for _, rule := range a.y.PortForwards {
			if rule.GuestSocket != "" {
				local := hostAddress(rule, guestagentapi.IPPort{})
				// using ctx.Background() because ctx has already been cancelled
				if err := forwardSSH(context.Background(), a.sshConfig, a.sshRemote, local, rule.GuestSocket, verbCancel); err != nil {
					mErr = multierror.Append(mErr, err)
				}
			}
		}
		return mErr
	})

	for {
		//		_, _, err := ssh.ExecuteScript(a.sshRemote, 22, a.sshConfig, `#!/bin/bash
		//true`, "Ping to keep SSH Master alive")
		//		if err != nil {
		//			logrus.Error("SSH Ping to guest failed", err)
		//		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (a *HostAgent) WatchGuestAgentEvents(ctx context.Context, conn socket.VsockConnection) {
	if err := a.processGuestAgentEvents(ctx, conn); err != nil {
		if !errors.Is(err, context.Canceled) {
			logrus.WithError(err).Warn("connection to the guest agent was closed unexpectedly")
		}
	}
}

func (a *HostAgent) processGuestAgentEvents(ctx context.Context, conn socket.VsockConnection) error {
	var first = true
	conn.ReadEvents(func(data string) {
		if first {
			var negotiate guestagentapi.Info
			err := json.Unmarshal([]byte(data), &negotiate)
			if err != nil {
				logrus.Error("Error during parse of negotiate")
				return
			}
			first = false
			logrus.Debugf("guest agent info: %+v", negotiate)
		} else {
			var event guestagentapi.Event
			err := json.Unmarshal([]byte(data), &event)
			if err != nil {
				logrus.Error("Error during parse of event")
				return
			}
			logrus.Debugf("guest agent event: %+v", event)
			for _, f := range event.Errors {
				logrus.Warnf("received error from the guest: %q", f)
			}
			sshRemoteUser := sshutil.SSHRemoteUser(*a.y.MACAddress)
			a.portForwarder.OnEvent(ctx, sshRemoteUser, event)
		}
	})
	return io.EOF
}

func (a *HostAgent) emitEvent(ctx context.Context, ev events.Event) {
	a.eventEncMu.Lock()
	defer a.eventEncMu.Unlock()
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	if err := a.eventEnc.Encode(ev); err != nil {
		logrus.Println("Emit")
		logrus.WithField("event", ev).WithError(err).Error("failed to emit an event")
	}
}

const (
	verbForward = "forward"
	verbCancel  = "cancel"
)

func forwardSSH(ctx context.Context, sshConfig *ssh.SSHConfig, userAndIp string, local, remote string, verb string) error {
	args := sshConfig.Args()
	args = append(args,
		"-T",
		"-O", verb,
		"-L", local+":"+remote,
		"-N",
		"-f",
		userAndIp,
		"--",
	)
	if strings.HasPrefix(local, "/") {
		switch verb {
		case verbForward:
			logrus.Infof("Forwarding %q (guest) to %q (host)", remote, local)
			if err := os.RemoveAll(local); err != nil {
				logrus.WithError(err).Warnf("Failed to clean up %q (host) before setting up forwarding", local)
			}
			if err := os.MkdirAll(filepath.Dir(local), 0750); err != nil {
				return fmt.Errorf("can't create directory for local socket %q: %w", local, err)
			}
		case verbCancel:
			logrus.Infof("Stopping forwarding %q (guest) to %q (host)", remote, local)
			defer func() {
				if err := os.RemoveAll(local); err != nil {
					logrus.WithError(err).Warnf("Failed to clean up %q (host) after stopping forwarding", local)
				}
			}()
		default:
			panic(fmt.Errorf("invalid verb %q", verb))
		}
	}
	logrus.Println("Forwarding=====", args)
	cmd := exec.CommandContext(ctx, sshConfig.Binary(), args...)
	if out, err := cmd.Output(); err != nil {
		if verb == verbForward && strings.HasPrefix(local, "/") {
			logrus.WithError(err).Warnf("Failed to set up forward from %q (guest) to %q (host)", remote, local)
			if removeErr := os.RemoveAll(local); err != nil {
				logrus.WithError(removeErr).Warnf("Failed to clean up %q (host) after forwarding failed", local)
			}
		}
		return fmt.Errorf("failed to run %v: %q: %w", cmd.Args, string(out), err)
	}
	return nil
}
