package osutil

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func GetIPFromMac(ctx context.Context, mac string) (string, error) {
	arpCmd := exec.CommandContext(ctx, "/bin/sh", "-c", fmt.Sprintf("arp -a | grep -w -i '%s' | awk '{print $2}'", mac))
	output, err := arpCmd.Output()
	if err != nil {
		return "", err
	}
	s := string(output)
	s = strings.Replace(s, "(", "", 1)
	s = strings.Replace(s, ")", "", 1)
	s = strings.Replace(s, "\n", "", 1)
	return s, nil
}
