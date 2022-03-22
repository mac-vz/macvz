package osutil

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func GetIPFromMac(ctx context.Context, mac string) (string, error) {
	mac = ProcessMacAddress(mac)
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

/*
This is needed because arp -a will strip the mac with trailing 0
Eg: 04:95:e6:69:ba:80 will be 4:95:e6:69:ba:80
	01:00:5e:00:00:fb will be 1:0:5e:0:0:fb
*/
func ProcessMacAddress(mac string) string {
	parts := strings.Split(mac, ":")
	for i, part := range parts {
		parts[i] = strings.TrimPrefix(part, "0")
	}
	return strings.Join(parts, ":")
}
