package hostagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/balaji113/macvz/pkg/socket"
	"github.com/balaji113/macvz/pkg/yaml"
	"github.com/lima-vm/sshocker/pkg/ssh"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	sshLocalPort  int
	instDir       string
	instName      string
	sshConfig     *ssh.SSHConfig
	portForwarder *portForwarder
	onClose       []func() error // LIFO

	sigintCh chan os.Signal

	vsock socket.VsockConnection

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

	sshLocalPort, err := determineSSHLocalPort(y, instName)
	if err != nil {
		return nil, err
	}

	sshOpts, err := sshutil.SSHOpts(inst.Dir, *y.SSH.LoadDotSSHPubKeys, *y.SSH.ForwardAgent)
	if err != nil {
		return nil, err
	}
	sshConfig := &ssh.SSHConfig{
		AdditionalArgs: sshutil.SSHArgsFromOpts(sshOpts),
	}

	rules := make([]yaml.PortForward, 0, 3+len(y.PortForwards))
	// Block ports 22 and sshLocalPort on all IPs
	for _, port := range []int{sshGuestPort, sshLocalPort} {
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
		y:             y,
		sshLocalPort:  sshLocalPort,
		instDir:       inst.Dir,
		instName:      instName,
		sshConfig:     sshConfig,
		portForwarder: newPortForwarder(sshConfig, sshLocalPort, rules),
		sigintCh:      sigintCh,
	}
	return a, nil
}

func determineSSHLocalPort(y *yaml.MacVZYaml, instName string) (int, error) {
	sshLocalPort, err := findFreeTCPLocalPort()
	if err != nil {
		return 0, fmt.Errorf("failed to find a free port, try setting `ssh.localPort` manually: %w", err)
	}
	return sshLocalPort, nil
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

func (a *HostAgent) StartHostAgentRoutines(ctx context.Context) error {
	a.onClose = append(a.onClose, func() error {
		logrus.Debugf("shutting down the SSH master")
		if exitMasterErr := ssh.ExitMaster("127.0.0.1", a.sshLocalPort, a.sshConfig); exitMasterErr != nil {
			logrus.WithError(exitMasterErr).Warn("failed to exit SSH master")
		}
		return nil
	})
	var mErr error
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
			_ = forwardSSH(ctx, a.sshConfig, a.sshLocalPort, local, rule.GuestSocket, verbForward)
		}
	}

	a.onClose = append(a.onClose, func() error {
		logrus.Debugf("Stop forwarding unix sockets")
		var mErr error
		for _, rule := range a.y.PortForwards {
			if rule.GuestSocket != "" {
				local := hostAddress(rule, guestagentapi.IPPort{})
				// using ctx.Background() because ctx has already been cancelled
				if err := forwardSSH(context.Background(), a.sshConfig, a.sshLocalPort, local, rule.GuestSocket, verbCancel); err != nil {
					mErr = multierror.Append(mErr, err)
				}
			}
		}
		return mErr
	})
}

func (a *HostAgent) WatchGuestAgentEvents(ctx context.Context) {
	for {
		if err := a.processGuestAgentEvents(ctx); err != nil {
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

func (a *HostAgent) processGuestAgentEvents(ctx context.Context) error {
	var negotiate guestagentapi.Info
	a.vsock.ReadEvents(func(data string) {
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

const (
	verbForward = "forward"
	verbCancel  = "cancel"
)

func forwardSSH(ctx context.Context, sshConfig *ssh.SSHConfig, port int, local, remote string, verb string) error {
	args := sshConfig.Args()
	args = append(args,
		"-T",
		"-O", verb,
		"-L", local+":"+remote,
		"-N",
		"-f",
		"-p", strconv.Itoa(port),
		"127.0.0.1",
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
