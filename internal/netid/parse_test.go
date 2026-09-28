package netid

import "testing"

// 已连接（英文界面）—— 注意同时存在 SSID 和 BSSID 行，最容易踩的坑。
const sampleConnectedEN = `
There is 1 interface on the system:

    Name                   : WLAN
    Description            : Intel(R) Wi-Fi 6 AX201 160MHz
    GUID                   : 76bb5800-c213-4070-8c76-d835f91f1311
    Physical address       : 04:f0:ee:13:4e:71
    Interface type         : Primary
    State                  : connected
    SSID                   : Corp-Dev
    BSSID                  : a4:2b:b0:11:22:33
    Network type           : Infrastructure
    Radio type             : 802.11ax
    Authentication         : WPA2-Personal
    Cipher                 : CCMP
`

// 已连接（中文界面）—— 值可能含中文，且标签本地化。
const sampleConnectedZH = `
系统上有 1 个接口:

    名称                   : WLAN
    描述                   : Intel(R) Wi-Fi 6 AX201 160MHz
    物理地址               : 04:f0:ee:13:4e:71
    状态                   : 已连接
    SSID                   : 公司内网-5G
    BSSID                  : a4:2b:b0:11:22:33
    网络类型               : 基础结构
    无线电类型             : 802.11ax
`

// 未连接 —— 输出里根本没有 SSID 行。
const sampleDisconnected = `
There is 1 interface on the system:

    Name                   : WLAN
    Description            : Intel(R) Wi-Fi 6 AX201 160MHz
    GUID                   : 76bb5800-c213-4070-8c76-d835f91f1311
    Physical address       : 04:f0:ee:13:4e:71
    Interface type         : Primary
    State                  : disconnected
    Radio status           : Hardware On
                             Software On
`

// 完全没有 WLAN 网卡。
const sampleNoInterface = `
There is no wireless interface on the system.
`

// SSID 名里带冒号 —— 必须只按第一个冒号切分。
const sampleSSIDWithColon = `
    State                  : connected
    SSID                   : Lab:2.4G
    BSSID                  : a4:2b:b0:11:22:33
`

func TestParseSSID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"英文已连接", sampleConnectedEN, "Corp-Dev"},
		{"中文已连接", sampleConnectedZH, "公司内网-5G"},
		{"未连接", sampleDisconnected, ""},
		{"无网卡", sampleNoInterface, ""},
		{"SSID 含冒号", sampleSSIDWithColon, "Lab:2.4G"},
		{"空输入", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseSSID(c.in); got != c.want {
				t.Errorf("parseSSID() = %q, want %q", got, c.want)
			}
		})
	}
}

// 关键的回归测试：SSID 行匹配绝不能命中 BSSID 行。
func TestParseSSIDNeverMatchesBSSID(t *testing.T) {
	// 只有 BSSID、没有 SSID（构造出来的边界情况）
	only := "    BSSID                  : a4:2b:b0:11:22:33\n"
	if got := parseSSID(only); got != "" {
		t.Errorf("不应把 BSSID 当作 SSID，得到 %q", got)
	}
}
