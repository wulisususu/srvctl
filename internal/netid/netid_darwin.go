//go:build darwin

package netid

import (
	"os/exec"
	"strings"
)

// ssidPlatform 在 macOS 上读当前 WiFi 名称。
//
// macOS 14+ 需要「定位服务」授权才能拿到 SSID；没有授权时这里返回空串，
// 上层会退化为"只按 TCP 可达性判定"，不会误报灰色。
func ssidPlatform() string {
	for _, dev := range []string{"en0", "en1"} {
		out, err := exec.Command("networksetup", "-getairportnetwork", dev).Output()
		if err != nil {
			continue
		}
		// 已连接时输出形如 "Current Wi-Fi Network: MyWiFi"
		// （非英文系统标签会本地化，所以只取冒号后的部分）。
		_, value, ok := strings.Cut(string(out), ":")
		if !ok {
			continue
		}
		v := strings.TrimSpace(value)
		// 未关联网络时这里是一句提示语，用长度和句号做个粗筛。
		if v == "" || strings.HasSuffix(v, ".") || len(v) > 40 {
			continue
		}
		return v
	}
	return ""
}
