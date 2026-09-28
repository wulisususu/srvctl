// Package config 负责解析应用的数据目录与文件路径。
//
// vault 路径优先级：
//  1. SRVCTL_VAULT 环境变量
//  2. portable 模式：可执行文件同目录
//  3. 默认：DataDir()
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const appDirName = "srvctl"

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
