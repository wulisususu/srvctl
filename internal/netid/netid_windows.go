//go:build windows

package netid

import "os/exec"

// ssidPlatform 通过 netsh 读取当前 WiFi 名称。
func ssidPlatform() string {
	raw, err := exec.Command("cmd", "/c", "chcp 65001 >nul && netsh wlan show interfaces").Output()
	if err != nil {
		return ""
	}
	return parseSSID(decodeConsole(raw))
}
