// Package config 负责解析应用的数据目录与文件路径。
//
// vault 路径优先级：
//  1. SRVCTL_VAULT 环境变量
//  2. portable 模式：可执行文件同目录
//  3. 默认：DataDir()
package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	appDirName = "srvctl"
	appName    = "srvctl"
)

// DataDir 返回应用数据目录。
//
//	Windows  %APPDATA%\srvctl
//	macOS    ~/Library/Application Support/srvctl
//	Linux    $XDG_CONFIG_HOME/srvctl 或 ~/.config/srvctl
func DataDir() string {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, appDirName)
		}
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appDirName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return appDirName
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, "AppData", "Roaming", appDirName)
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", appDirName)
	default:
		return filepath.Join(home, ".config", appDirName)
	}
}

// ExeDir 返回可执行文件所在目录（portable 模式用）。
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// VaultPath 解析 vault 文件位置。
func VaultPath(portable bool) string {
	if p := os.Getenv("SRVCTL_VAULT"); p != "" {
		return p
	}
	if portable {
		return filepath.Join(ExeDir(), "vault.enc")
	}
	return filepath.Join(DataDir(), "vault.enc")
}

// SessionKeyPath 是 `srvctl login` 写入主密码的位置，供无交互的 CLI 调用
// （AI 调用 `srvctl exec` 时走这条路）。
func SessionKeyPath(portable bool) string {
	if portable {
		return filepath.Join(ExeDir(), "session.key")
	}
	return filepath.Join(DataDir(), "session.key")
}

// LogPath 是 GUI 模式（无控制台）的日志文件位置。
func LogPath(portable bool) string {
	if portable {
		return filepath.Join(ExeDir(), "srvctl.log")
	}
	return filepath.Join(DataDir(), "srvctl.log")
}

// CLICommand 返回"给 AI / 脚本用的 srvctl 调用方式"。
//
// 优先返回裸命令名 srvctl —— 前提是它确实能在 PATH 上找到。
// 找不到就退回完整路径（当前可执行文件旁边的 srvctl[.exe]）。
//
// 为什么必须这么做：生成给 AI 的说明里如果写了一个跑不起来的命令，
// AI 只会得到 "command not found"，然后开始自己猜怎么连服务器 ——
// 那正是这个工具要避免的事。宁可路径长一点，也要保证能跑。
func CLICommand() string {
	if _, err := exec.LookPath(appName); err == nil {
		return appName
	}

	exe, err := os.Executable()
	if err != nil {
		return appName
	}
	name := appName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	cand := filepath.Join(filepath.Dir(exe), name)
	if _, err := os.Stat(cand); err != nil {
		return appName
	}

	// 路径含空格时必须加引号，否则 AI 拼出来的命令会断成两截
	if strings.ContainsAny(cand, " \t") {
		return `"` + cand + `"`
	}
	return cand
}

// CLICommandIsOnPath 报告 CLICommand() 返回的是裸命令名还是完整路径。
// 用于在说明文本里解释"为什么这里是个长路径"。
func CLICommandIsOnPath() bool {
	return CLICommand() == appName
}
