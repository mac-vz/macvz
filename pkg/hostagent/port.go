package hostagent

import (
	"context"
	"github.com/balaji113/macvz/pkg/yaml"
	"net"

	"github.com/balaji113/macvz/pkg/guestagent/api"
	"github.com/lima-vm/sshocker/pkg/ssh"
	"github.com/sirupsen/logrus"
)

type portForwarder struct {
	sshConfig *ssh.SSHConfig
	rules     []yaml.PortForward
}

func newPortForwarder(sshConfig *ssh.SSHConfig, rules []yaml.PortForward) *portForwarder {
	return &portForwarder{
		sshConfig: sshConfig,
		rules:     rules,
	}
}

func hostAddress(rule yaml.PortForward, guest api.IPPort) string {
	if rule.HostSocket != "" {
		return rule.HostSocket
	}
	host := api.IPPort{IP: rule.HostIP}
	if guest.Port == 0 {
		// guest is a socket
		host.Port = rule.HostPort
	} else {
		host.Port = guest.Port + rule.HostPortRange[0] - rule.GuestPortRange[0]
	}
	return host.String()
}

func (pf *portForwarder) forwardingAddresses(guest api.IPPort) (string, string) {
	for _, rule := range pf.rules {
		if rule.GuestSocket != "" {
			continue
		}
		if guest.Port < rule.GuestPortRange[0] || guest.Port > rule.GuestPortRange[1] {
			continue
		}
		switch {
		case guest.IP.IsUnspecified():
		case guest.IP.Equal(rule.GuestIP):
		case guest.IP.Equal(net.IPv6loopback) && rule.GuestIP.Equal(api.IPv4loopback1):
		case rule.GuestIP.IsUnspecified() && !rule.GuestIPMustBeZero:
			// When GuestIPMustBeZero is true, then 0.0.0.0 must be an exact match, which is already
			// handled above by the guest.IP.IsUnspecified() condition.
		default:
			continue
		}
		if rule.Ignore {
			if guest.IP.IsUnspecified() && !rule.GuestIP.IsUnspecified() {
				continue
			}
			break
		}
		return hostAddress(rule, guest), guest.String()
	}
	return "", guest.String()
}

func (pf *portForwarder) OnEvent(ctx context.Context, sshRemote string, ev api.Event) {
	for _, f := range ev.LocalPortsRemoved {
		local, remote := pf.forwardingAddresses(f)
		if local == "" {
			continue
		}
		logrus.Infof("Stopping forwarding TCP from %s to %s", remote, local)
		if err := forwardTCP(ctx, pf.sshConfig, sshRemote, local, remote, verbCancel); err != nil {
			logrus.WithError(err).Warnf("failed to stop forwarding tcp port %d", f.Port)
		}
	}
	for _, f := range ev.LocalPortsAdded {
		local, remote := pf.forwardingAddresses(f)
		if local == "" {
			logrus.Infof("Not forwarding TCP %s", remote)
			continue
		}
		logrus.Infof("Forwarding TCP from %s to %s", remote, local)
		if err := forwardTCP(ctx, pf.sshConfig, sshRemote, local, remote, verbForward); err != nil {
			logrus.WithError(err).Warnf("failed to set up forwarding tcp port %d (negligible if already forwarded)", f.Port)
		}
	}
}
