// Command ssidtest 是 WiFi 名称读取的验证工具。
//
// 存在的理由：netid 包在 Windows 上依赖 netsh 的输出格式与编码，
// 这是整个项目里唯一无法离线验证的点。在目标机器上跑一次它，
// 确认能读到正确的 SSID，再继续后面的开发。
//
//	srvctl-ssidtest          读一次
//	srvctl-ssidtest -raw     同时打印 netsh 原始输出，便于排查
package main

import (
	"flag"
	"fmt"
	"os/exec"
	"strings"

	"srvctl/internal/netid"
)

func main() {
	raw := flag.Bool("raw", false, "打印 netsh 原始输出")
	flag.Parse()

	fmt.Println("=== srvctl SSID 探测验证 ===")
	fmt.Println()

	ssid := netid.SSID()
	if ssid == "" {
		fmt.Println("结果: 读取失败或无法确定")
		fmt.Println()
		fmt.Println("可能原因：")
		fmt.Println("  - 这台机器用有线连接，没有连接 WiFi")
		fmt.Println("  - 没有 WLAN 网卡")
		fmt.Println("  - netsh 输出格式与预期不同（用 -raw 看原始输出）")
		fmt.Println()
		fmt.Println("注意：读取失败不会导致误判 —— 服务器不会被标成灰色，")
		fmt.Println("      只会退化为单纯按 TCP 可达性判断。")
	} else {
		fmt.Printf("结果: %q\n", ssid)
		fmt.Println("✅ 读取成功")
	}

	if *raw {
		fmt.Println()
		fmt.Println("=== netsh wlan show interfaces 原始输出 ===")
		out, err := exec.Command("cmd", "/c", "chcp 65001 >nul && netsh wlan show interfaces").Output()
		if err != nil {
			fmt.Printf("执行失败: %v\n", err)
			return
		}
		// 直接按字节打印，保留原始编码，方便肉眼判断是不是 GBK
		fmt.Println(strings.ReplaceAll(string(out), "\r\n", "\n"))
	}
}
