// Package config 负责解析应用的数据目录与文件路径。
//
// vault 路径优先级：
//  1. SRVCTL_VAULT 环境变量
//  2. portable 模式：可执行文件同目录
//  3. 默认：DataDir()
package config

import (
	"encoding/json"
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

// ---------- 持久化配置 ----------

// FileConfig 是存在 DataDir()/config.json 的用户配置。
//
// 存在的理由：双击启动的 GUI 拿不到命令行参数，所以必须有个地方能持久地
// 记住"vault 放在哪"。否则「把 vault 放进云盘目录，多台机器共用」这条路
// 根本走不通 —— 每次都得靠 --vault 或环境变量，而双击时两者都没有。
type FileConfig struct {
	// VaultPath 显式指定的 vault 位置；空表示用默认位置。
	VaultPath string `json:"vault_path,omitempty"`
}

// ConfigPath 返回配置文件路径。
//
// 它永远在本地数据目录，**不跟着 vault 走** —— vault 可能是共享的，
// 但"共享目录挂在哪"这件事每台机器本来就不同。
func ConfigPath() string {
	return filepath.Join(DataDir(), "config.json")
}

// ReadConfig 读取配置。文件不存在或内容损坏时返回零值且不报错 ——
// 一个坏掉的配置文件不该让程序完全起不来。
func ReadConfig() FileConfig {
	var c FileConfig
	raw, err := os.ReadFile(ConfigPath())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(raw, &c)
	return c
}

// WriteConfig 覆盖写入配置。
func WriteConfig(c FileConfig) error {
	if err := os.MkdirAll(DataDir(), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(), append(raw, '\n'), 0o600)
}

// VaultSource 说明当前 vault 路径由什么决定，供 `srvctl config` 展示。
func VaultSource(portable bool) string {
	switch {
	case os.Getenv("SRVCTL_VAULT") != "":
		return "环境变量 SRVCTL_VAULT"
	case portable:
		return "便携模式（可执行文件同目录）"
	case ReadConfig().VaultPath != "":
		return "配置文件 " + ConfigPath()
	default:
		return "默认位置"
	}
}

// VaultIsConfigured 报告 vault 路径是不是被显式指定的（而不是用默认位置）。
//
// 界面靠它区分两种"没有 vault"：真的第一次用（该引导创建），
// 还是配置了一个还没同步下来的路径（该警告 —— 这时创建会覆盖别人的数据）。
func VaultIsConfigured(portable bool) bool {
	if os.Getenv("SRVCTL_VAULT") != "" {
		return true
	}
	return !portable && ReadConfig().VaultPath != ""
}

// VaultPath 解析 vault 文件位置。
//
// 优先级：环境变量 > 便携模式 > 配置文件 > 默认位置。
// 前两个都是"这一次运行"的显式意图，所以排在持久配置前面。
func VaultPath(portable bool) string {
	if p := os.Getenv("SRVCTL_VAULT"); p != "" {
		return p
	}
	if portable {
		return filepath.Join(ExeDir(), "vault.enc")
	}
	if p := ReadConfig().VaultPath; p != "" {
		return p
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
