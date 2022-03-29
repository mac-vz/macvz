//go:build !darwin
// +build !darwin

package hostagent

import (
	"context"

	"github.com/lima-vm/sshocker/pkg/ssh"
)

func forwardTCP(ctx context.Context, sshConfig *ssh.SSHConfig, userAndIp string, local, remote string, verb string) error {
	return forwardSSH(ctx, sshConfig, userAndIp, local, remote, verb)
}
