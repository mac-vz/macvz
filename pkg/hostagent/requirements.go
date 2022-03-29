package hostagent

import (
	"bytes"
	"context"
	"fmt"
	"github.com/balaji113/macvz/pkg/osutil"
	"github.com/lima-vm/sshocker/pkg/ssh"
	"os/exec"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/lima-vm/lima/pkg/limayaml"
	"github.com/sirupsen/logrus"
)

func (a *HostAgent) waitForRequirements(ctx context.Context, label string, requirements []requirement) error {
	const (
		retries       = 60
		sleepDuration = 10 * time.Second
	)
	var mErr error

	for i, req := range requirements {
	retryLoop:
		for j := 0; j < retries; j++ {
			logrus.Infof("Waiting for the %s requirement %d of %d: %q", label, i+1, len(requirements), req.description)
			err := a.waitForRequirement(ctx, req)
			if err == nil {
				logrus.Infof("The %s requirement %d of %d is satisfied", label, i+1, len(requirements))
				break retryLoop
			}
			if req.fatal {
				logrus.Infof("No further %s requirements will be checked", label)
				return multierror.Append(mErr, fmt.Errorf("failed to satisfy the %s requirement %d of %d %q: %s; skipping further checks: %w", label, i+1, len(requirements), req.description, req.debugHint, err))
			}
			if j == retries-1 {
				mErr = multierror.Append(mErr, fmt.Errorf("failed to satisfy the %s requirement %d of %d %q: %s: %w", label, i+1, len(requirements), req.description, req.debugHint, err))
				break retryLoop
			}
			time.Sleep(10 * time.Second)
		}
	}
	return mErr
}

func (a *HostAgent) waitForRequirement(ctx context.Context, r requirement) error {
	logrus.Debugf("executing script %q", r.description)
	if r.host {
		bashCmd := exec.Command("bash")
		bashCmd.Stdin = strings.NewReader(r.script)
		var stderr bytes.Buffer
		bashCmd.Stderr = &stderr
		stdout, err := bashCmd.Output()
		logrus.Debugf("stdout=%q, stderr=%q, err=%v", stdout, stderr, err)
		if err != nil {
			return fmt.Errorf("stdout=%q, stderr=%q: %w", stdout, stderr, err)
		}
	} else {
		stdout, stderr, err := ssh.ExecuteScript(a.sshRemote, 22, a.sshConfig, r.script, r.description)
		logrus.Debugf("stdout=%q, stderr=%q, err=%v", stdout, stderr, err)
		if err != nil {
			return fmt.Errorf("stdout=%q, stderr=%q: %w", stdout, stderr, err)
		}
	}
	return nil
}

type requirement struct {
	description string
	script      string
	debugHint   string
	fatal       bool
	host        bool
}

func (a *HostAgent) hostRequirements() []requirement {
	req := make([]requirement, 0)
	req = append(req,
		requirement{
			description: "Host IP Bind",
			script: fmt.Sprintf(`#!/bin/bash
if [[ $( arp -a | grep -w -i '%s' | awk '{print $2}') ]]; then 
  exit 0 
else 
  exit 1 
fi
`, osutil.TrimMACAddress(*a.y.MACAddress)),
			debugHint: `Failed to acquire host IP.
`,
			host: true,
		})
	return req
}

func (a *HostAgent) essentialRequirements() []requirement {
	req := make([]requirement, 0)
	req = append(req,
		requirement{
			description: "ssh",
			script: `#!/bin/bash
true
`,
			debugHint: `Failed to SSH into the guest.
If any private key under ~/.ssh is protected with a passphrase, you need to have ssh-agent to be running.
`,
		})
	req = append(req, requirement{
		description: "user session is ready for ssh",
		script: `#!/bin/bash
set -eux -o pipefail
if ! timeout 30s bash -c "until sudo diff -q /run/macvz-ssh-ready /mnt/cidata/meta-data 2>/dev/null; do sleep 3; done"; then
	echo >&2 "not ready to start persistent ssh session"
	exit 1
fi
`,
		debugHint: `The boot sequence will terminate any existing user session after updating
/etc/environment to make sure the session includes the new values.
Terminating the session will break the persistent SSH tunnel, so
it must not be created until the session reset is done.
`,
	})
	//	req = append(req, requirement{
	//		description: "the guest agent to be running",
	//		script: `#!/bin/bash
	//set -eux -o pipefail
	//sock="/run/lima-guestagent.sock"
	//if ! timeout 30s bash -c "until [ -S \"${sock}\" ]; do sleep 3; done"; then
	//	echo >&2 "lima-guestagent is not installed yet"
	//	exit 1
	//fi
	//`,
	//		debugHint: `The guest agent (/run/lima-guestagent.sock) does not seem running.
	//Make sure that you are using an officially supported image.
	//Also see "/var/log/cloud-init-output.log" in the guest.
	//A possible workaround is to run "lima-guestagent install-systemd" in the guest.
	//`,
	//	})
	return req
}

func (a *HostAgent) optionalRequirements() []requirement {
	req := make([]requirement, 0)
	for _, probe := range a.y.Probes {
		if probe.Mode == limayaml.ProbeModeReadiness {
			req = append(req, requirement{
				description: probe.Description,
				script:      probe.Script,
				debugHint:   probe.Hint,
			})
		}
	}
	return req
}

func (a *HostAgent) finalRequirements() []requirement {
	req := make([]requirement, 0)
	req = append(req,
		requirement{
			description: "boot scripts must have finished",
			script: `#!/bin/bash
set -eux -o pipefail
if ! timeout 30s bash -c "until sudo diff -q /run/lima-boot-done /mnt/cidata/meta-data 2>/dev/null; do sleep 3; done"; then
	echo >&2 "boot scripts have not finished"
	exit 1
fi
`,
			debugHint: `All boot scripts, provisioning scripts, and readiness probes must
finish before the instance is considered "ready".
Check "/var/log/cloud-init-output.log" in the guest to see where the process is blocked!
`,
		})
	return req
}
