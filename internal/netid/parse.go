package netid

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// decodeConsole 把命令输出转成 UTF-8。
//
// chcp 65001 只影响控制台代码页。当进程没有控制台（srvctl-gui.exe 用
// -H=windowsgui 构建）或 stdout 被重定向到管道时，netsh 会退回 OEM 代码页
// —— 中文 Windows 上是 GBK(936)。所以拿到字节后必须检查是不是合法 UTF-8。
func decodeConsole(raw []byte) string {
	if utf8.Valid(raw) {
		return string(raw)
	}
	if decoded, _, err := transform.String(simplifiedchinese.GBK.NewDecoder(), string(raw)); err == nil {
		return decoded
	}
	return string(raw)
}

// parseSSID 从 netsh wlan show interfaces 的输出里提取当前 SSID。
//
// 未连接 WiFi 时输出里根本没有 SSID 行（只有 State: disconnected），
// 此时返回空串 —— 上层必须把它当作"未知网络"，而不是"不匹配"。
//
// 中文 Windows 上 netsh 的部分标签会本地化，但 "SSID" / "BSSID" 保持英文，
// 所以按前缀匹配是稳的。
func parseSSID(text string) string {
	const label = "SSID"
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, label) {
			continue
		}
		// 排除 BSSID（HasPrefix 已排除，双保险），也排除 "SSID 数量"
		// 这类以 SSID 开头但后面不跟分隔符的行。
		rest := line[len(label):]
		if rest == "" {
			continue
		}
		if rest[0] != ' ' && rest[0] != '\t' && rest[0] != ':' {
			continue
		}
		_, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if v := strings.TrimSpace(value); v != "" {
			return v
		}
	}
	return ""
}
