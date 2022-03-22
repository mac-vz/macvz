package hostagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/balaji113/macvz/pkg/hostagent/events"
	"github.com/balaji113/macvz/pkg/osutil"
	"github.com/balaji113/macvz/pkg/socket"
	"github.com/balaji113/macvz/pkg/vzrun"
	"github.com/balaji113/macvz/pkg/yaml"
	"github.com/lima-vm/sshocker/pkg/ssh"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	guestagentapi "github.com/balaji113/macvz/pkg/guestagent/api"
	"github.com/balaji113/macvz/pkg/sshutil"
	"github.com/balaji113/macvz/pkg/store"
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
)

type HostAgent struct {
	y             *yaml.MacVZYaml
	instDir       string
	instName      string
	sshConfig     *ssh.SSHConfig
	portForwarder *portForwarder
	onClose       []func() error // LIFO

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

	logrus.Println("New host")
	sshOpts, err := sshutil.SSHOpts(inst.Dir, *y.SSH.LoadDotSSHPubKeys, *y.SSH.ForwardAgent)
	if err != nil {
		return nil, err
	}
	sshConfig := &ssh.SSHConfig{
		AdditionalArgs: sshutil.SSHArgsFromOpts(sshOpts),
	}

	a := &HostAgent{
		y:         y,
		instDir:   inst.Dir,
		instName:  instName,
		sshConfig: sshConfig,
		sigintCh:  sigintCh,
		eventEnc:  json.NewEncoder(os.Stdout),
	}

	return a, nil
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

	logrus.Println("Booting")
	stBooting := events.Status{}
	a.emitEvent(ctx, events.Event{Status: stBooting})

	ctxHA, cancelHA := context.WithCancel(ctx)
	go func() {
		initialize, err := vzrun.Initialize(a.instName)
		if err != nil {
			logrus.Fatal("INIT", err)
		}
		err = vzrun.Run(*initialize, a.sigintCh, func(ctx context.Context, vsock socket.VsockConnection) {
			a.WatchGuestAgentEvents(ctx, vsock)
		})
		logrus.Println("INFOOO")
		if err != nil {
			logrus.Fatal("RUN", err)
		}
	}()

	go func() {
		stRunning := events.Status{}
		if haErr := a.startHostAgentRoutines(ctxHA); haErr != nil {
			stRunning.Degraded = true
			stRunning.Errors = append(stRunning.Errors, haErr.Error())
		}
		stRunning.Running = true
		a.emitEvent(ctx, events.Event{Status: stRunning})
	}()

	for {
		select {
		case <-a.sigintCh:
			logrus.Info("Received SIGINT, shutting down the host agent")
			cancelHA()
		}
	}
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
	mac, _ := osutil.GetIPFromMac(ctx, *a.y.MACAddress)
	user, _ := osutil.MacVZUser(true)
	a.sshRemote = user.Username + "@" + mac

	rules := make([]yaml.PortForward, 0, 1+len(a.y.PortForwards))
	rules = append(rules, a.y.PortForwards...)

	// Default forwards for all non-privileged ports from "127.0.0.1" and "::1"
	rule := yaml.PortForward{GuestIP: guestagentapi.IPv4loopback1}
	yaml.FillPortForwardDefaults(&rule, a.instDir)
	rules = append(rules, rule)
	a.portForwarder = newPortForwarder(a.sshConfig, a.sshRemote, rules)

	if err := a.waitForRequirements(ctx, "essential", a.essentialRequirements()); err != nil {
		mErr = multierror.Append(mErr, err)
	}
	go a.ForwardDefinedSockets(ctx)
	if err := a.waitForRequirements(ctx, "optional", a.optionalRequirements()); err != nil {
		mErr = multierror.Append(mErr, err)
	}
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
}

func (a *HostAgent) WatchGuestAgentEvents(ctx context.Context, conn socket.VsockConnection) {
	for {
		if err := a.processGuestAgentEvents(ctx, conn); err != nil {
			if !errors.Is(err, context.Canceled) {
				logrus.WithError(err).Warn("connection to the guest agent was closed unexpectedly")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (a *HostAgent) processGuestAgentEvents(ctx context.Context, conn socket.VsockConnection) error {
	var negotiate guestagentapi.Info
	conn.ReadEvents(func(data string) {
		logrus.Println("======DATA=========", data)
		if &negotiate == nil {
			err := json.Unmarshal([]byte(data), &negotiate)
			if err != nil {
				logrus.Error("Error during parse of negotiate")
			}
			logrus.Debugf("guest agent info: %+v", negotiate)
		} else {
			var event guestagentapi.Event
			err := json.Unmarshal([]byte(data), &event)
			if err != nil {
				logrus.Error("Error during parse of event")
			}
			logrus.Debugf("guest agent event: %+v", event)
			for _, f := range event.Errors {
				logrus.Warnf("received error from the guest: %q", f)
			}
			a.portForwarder.OnEvent(ctx, event)
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
