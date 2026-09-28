// Package netid 识别当前所处的网络环境。
//
// 目前只需要一件事：当前连接的 WiFi 名称（SSID）。
//
// 约定：SSID() 返回空字符串表示"无法确定"，调用方**必须**把它当作未知，
// 而不是"不匹配"。否则有线连接、无 WLAN 网卡的机器、或 macOS 缺少定位
// 权限时，会把所有要求特定 WiFi 的服务器误判成灰色。
package netid

// SSID 返回当前连接的 WiFi 名称，无法确定时返回空字符串。
func SSID() string {
	return ssidPlatform()
}
