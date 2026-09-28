// Package snippet 生成"复制给 AI"的连接说明。
//
// 这是整个工具的核心产物：用户点一下「复制给 AI」，粘进任意 AI 客户端的
// 对话框，AI 立刻知道该用什么方式连接，不需要用户复述 IP / 账号，也不需要
// AI 自己写一段 Python 去连。
package snippet

import (
	"fmt"
	"sort"
	"strings"

	"srvctl/internal/model"
	"srvctl/internal/reach"
)

// For 生成单条服务器的连接说明。
//
// cmd 是调用 srvctl 的方式（见 config.CLICommand）：
// 通常就是裸命令名 "srvctl"，但如果它不在 PATH 上，会是完整路径。
// 空字符串按 "srvctl" 处理。
func For(s model.Server, r *reach.Result, currentSSID, cmd string) string {
	cmd = normalizeCmd(cmd)
	onPath := cmd == "srvctl"

	var b strings.Builder

	platform := "Linux"
	if s.Platform == model.PlatformWindows {
		platform = "Windows"
	}

	if s.Platform == model.PlatformWindows {
		fmt.Fprintf(&b, "服务器 %s（%s，%s）已登记在 srvctl（仅登记，不检测连通性）。\n\n",
			s.Name, platform, s.Host)
		// mstsc 不带端口默认走 3389；只有非默认端口才需要显式写出来
		rdpTarget := s.Host
		if s.Port > 0 && s.Port != model.DefaultRDPPort {
			rdpTarget = fmt.Sprintf("%s:%d", s.Host, s.Port)
		}
		fmt.Fprintf(&b, "远程桌面：\n    mstsc /v:%s\n\n", rdpTarget)
		fmt.Fprintf(&b, "如果该机已启用 OpenSSH Server，也可以用：\n    %s exec %s \"<PowerShell 命令>\"\n",
			cmd, s.Name)
	} else {
		fmt.Fprintf(&b, "服务器 %s（%s，%s）已登记在 srvctl。\n\n",
			s.Name, platform, s.Address())

		// srvctl 不在 PATH 上时必须点明，否则 AI 看到一长串路径会
		// 以为可以简化成 `srvctl`，一试就是 command not found。
		if onPath {
			fmt.Fprintf(&b, "执行命令：\n    %s exec %s \"<命令>\"\n\n", cmd, s.Name)
		} else {
			fmt.Fprintf(&b, "执行命令（srvctl 未加入 PATH，请照抄下面的完整路径，不要简写成 srvctl）：\n")
			fmt.Fprintf(&b, "    %s exec %s \"<命令>\"\n\n", cmd, s.Name)
		}
		fmt.Fprintf(&b, "交互式会话：\n    %s shell %s\n\n", cmd, s.Name)
		b.WriteString("凭据由 srvctl 管理，不需要也不应该向我索要密码或私钥。\n")
	}

	writeMeta(&b, s, r, currentSSID)
	return b.String()
}

// normalizeCmd 兜底：空字符串或纯空白都按裸命令名处理。
func normalizeCmd(cmd string) string {
	if strings.TrimSpace(cmd) == "" {
		return "srvctl"
	}
	return cmd
}

// ForAll 生成全部服务器的索引（一次性贴给 AI，让它自己挑）。
func ForAll(servers []model.Server, results map[string]reach.Result, currentSSID, cmd string) string {
	cmd = normalizeCmd(cmd)

	sorted := append([]model.Server(nil), servers...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Category != sorted[j].Category {
			return sorted[i].Category < sorted[j].Category
		}
		return sorted[i].Name < sorted[j].Name
	})

	var b strings.Builder
	fmt.Fprintf(&b, "以下 %d 台服务器已登记在 srvctl（当前网络：%s）。\n", len(sorted), displaySSID(currentSSID))
	fmt.Fprintf(&b, "用 `%s exec <名称> \"<命令>\"` 执行命令，`%s shell <名称>` 开交互式会话。\n", cmd, cmd)
	if cmd != "srvctl" {
		fmt.Fprintf(&b, "（srvctl 未加入 PATH，必须使用上面的完整路径。）\n")
	}
	b.WriteString("凭据由 srvctl 管理，不需要也不应该向我索要密码或私钥。\n\n")

	lastCat := "\x00"
	for _, s := range sorted {
		cat := s.Category
		if cat == "" {
			cat = "(未分类)"
		}
		if cat != lastCat {
			fmt.Fprintf(&b, "\n[%s]\n", cat)
			lastCat = cat
		}
		state := StateLabel(results[s.Name])
		platform := "Linux"
		if s.Platform == model.PlatformWindows {
			platform = "Windows"
		}
		fmt.Fprintf(&b, "  %-20s %-22s %-8s %s\n", s.Name, s.Host, platform, state)
	}
	return b.String()
}

// writeMeta 追加分类/标签/网络/状态等元信息。
func writeMeta(b *strings.Builder, s model.Server, r *reach.Result, currentSSID string) {
	lines := make([]string, 0, 4)

	if s.Category != "" {
		lines = append(lines, "分类："+s.Category)
	}
	if len(s.Tags) > 0 {
		lines = append(lines, "标签："+strings.Join(s.Tags, ", "))
	}
	if s.Username != "" {
		lines = append(lines, "账号："+s.Username)
	}
	if s.RequiredSSID != "" {
		net := fmt.Sprintf("需要 WiFi：%s", s.RequiredSSID)
		if currentSSID != "" {
			if currentSSID == s.RequiredSSID {
				net += "（当前已满足 ✓）"
			} else {
				net += fmt.Sprintf("（当前：%s ✗）", currentSSID)
			}
		}
		lines = append(lines, net)
	}
	if r != nil && s.Checkable() {
		label := StateLabel(*r)
		if r.Note != "" {
			label += " —— " + r.Note
		}
		lines = append(lines, "状态："+label)
	}

	if len(lines) > 0 {
		b.WriteString("\n")
		for _, l := range lines {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}

	// 备注单独成块。
	//
	// 这是用户为这台机器写的上下文（用途、配置、到期时间、踩过的坑），
	// 正是 AI 接手前最该知道的东西 —— 混在元信息那一行里会把多行备注
	// 压成一坨，所以缩进后单独列。
	if n := strings.TrimSpace(s.Notes); n != "" {
		b.WriteString("\n备注：\n")
		for _, ln := range strings.Split(n, "\n") {
			b.WriteString("    " + strings.TrimRight(ln, "\r") + "\n")
		}
	}
}

// StateLabel 把探测结果转成给人/AI 看的中文描述。
func StateLabel(r reach.Result) string {
	switch r.State {
	case reach.StateOK:
		return fmt.Sprintf("可连接（%d ms）", r.LatencyMs)
	case reach.StateDown:
		return "不可连接"
	case reach.StateWrongNet:
		return "当前网络环境不满足"
	default:
		return "不检测"
	}
}

func displaySSID(ssid string) string {
	if ssid == "" {
		return "未知"
	}
	return ssid
}
