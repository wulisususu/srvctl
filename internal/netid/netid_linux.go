//go:build linux

package netid

import (
	"os/exec"
	"strings"
)

// ssidPlatform 在 Linux 上先试 iwgetid，再退到 nmcli。
func ssidPlatform() string {
	if out, err := exec.Command("iwgetid", "-r").Output(); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			return v
		}
	}

	out, err := exec.Command("nmcli", "-t", "-f", "active,ssid", "dev", "wifi").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		active, ssid, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && active == "yes" {
			return strings.TrimSpace(ssid)
		}
	}
	return ""
}
