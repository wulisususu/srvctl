// Package app 把 CLI 与 GUI 两个入口接到同一套核心逻辑上。
//
// 两个二进制（srvctl.exe / srvctl-gui.exe）共用本包，只有 main 包不同：
//   - srvctl.exe      控制台子系统，给命令行和 AI 用
//   - srvctl-gui.exe  windowsgui 子系统，双击启动界面且不弹黑框
package app

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"golang.org/x/term"

	"srvctl/internal/config"
	"srvctl/internal/sessionkey"
	"srvctl/internal/vault"
)

const appName = "srvctl"

var version = "0.1.0"

// errNeedLogin 在没有可用主密码且不允许交互时返回。
var errNeedLogin = errors.New(
	"vault 已锁定，且没有可用的主密码\n" +
		"  解决方式（任选其一）：\n" +
		"    1. 运行 `srvctl login` 记住主密码（AI 调用 CLI 需要这个）\n" +
		"    2. 设置环境变量 SRVCTL_PASSWORD")

// Version 返回版本号，若二进制带 VCS 信息则一并显示。
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return version + " (" + s.Value[:7] + ")"
			}
		}
	}
	return version
}

// globals 是所有子命令共用的全局开关。
type globals struct {
	portable bool
	vaultP   string
}

// parseGlobals 抽走全局开关，返回剩余参数。
// 允许全局开关出现在任意位置，这样 `srvctl list --portable` 也能用。
func parseGlobals(args []string) (globals, []string) {
	var g globals
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--portable", "-p":
			g.portable = true
		case "--vault":
			if i+1 < len(args) {
				i++
				g.vaultP = args[i]
			}
		default:
			rest = append(rest, args[i])
		}
	}
	return g, rest
}

func openStore(g globals) *vault.Store {
	p := g.vaultP
	if p == "" {
		p = config.VaultPath(g.portable)
	}
	return vault.Open(p)
}

// unlockStore 用可用凭据解锁 vault。
//
// allowPrompt=false 的场景是 AI 调用（exec / snippet 等）—— 那时没有终端，
// 交互输入会挂住。此时只接受 SRVCTL_PASSWORD 或 session.key。
func unlockStore(st *vault.Store, g globals, allowPrompt bool) error {
	if st.Unlocked() {
		return nil
	}
	if !st.Exists() {
		return fmt.Errorf("vault 不存在: %s\n请先运行 `%s init` 创建", st.Path(), appName)
	}

	// 1. 环境变量优先（CI / 一次性脚本用）
	if pw := os.Getenv("SRVCTL_PASSWORD"); pw != "" {
		return st.Unlock(pw)
	}

	// 2. `srvctl login` 记住的主密码
	if pw, ok := sessionkey.Load(config.SessionKeyPath(g.portable)); ok {
		if err := st.Unlock(pw); err == nil {
			return nil
		}
		// 记住的密码失效（例如在另一台机器上改了主密码），继续往下走
	}

	// 3. 交互输入
	if !allowPrompt {
		return errNeedLogin
	}
	pw, err := promptPassword("主密码: ")
	if err != nil {
		return err
	}
	if err := st.Unlock(pw); err != nil {
		return err
	}
	return nil
}

func promptPassword(label string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("标准输入不是终端，无法交互输入主密码")
	}
	fmt.Fprint(os.Stderr, label)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// passwordFromEnv 返回 SRVCTL_PASSWORD，未设置时为空串。
//
// 存在两个用途：脚本化初始化（CI / 批量部署），以及 AI 调用 CLI。
// 设置了这个变量时，所有需要主密码的命令都不再交互提示。
func passwordFromEnv() string {
	return os.Getenv("SRVCTL_PASSWORD")
}

// ---------- 终端对齐辅助 ----------
//
// 中文是双宽字符，直接用 %-20s 会错位，所以自己按显示宽度填充。

func padRight(s string, width int) string {
	w := displayWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if isWideRune(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD:
		return true
	}
	return false
}
