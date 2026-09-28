// Package sessionkey 管理"记住主密码"文件。
//
// 这是整个工具唯一的安全折衷点：AI 通过 CLI 调用 `srvctl exec` 时无法交互
// 输入主密码，所以需要一个存在磁盘上的凭据。
//
// 权衡说明：
//   - 该文件与 vault 同目录，权限 0600（Windows 上由目录 ACL 保护）
//   - 文件里存的是**主密码明文**，不是派生密钥 —— 这样用户改了 vault 密码
//     后，只需重新 login 一次
//   - 对比"把 IP/账号/密码发到微信当记事本"的现状，这已经是数量级的改进
//   - 不想要它：`srvctl logout` 删除即可，之后 CLI 会要求交互输入
//
// 后续升级路径：接入操作系统钥匙串（Windows 凭据管理器 / macOS Keychain /
// Linux secret-service），届时这个包换掉实现即可，调用方不用动。
package sessionkey

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Save 把主密码写入 path。
func Save(path, password string) error {
	if password == "" {
		return fmt.Errorf("密码为空，拒绝写入")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(password), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load 读取主密码。第二个返回值为 false 表示文件不存在或为空。
func Load(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	pw := strings.TrimRight(string(raw), "\r\n")
	if pw == "" {
		return "", false
	}
	return pw, true
}

// Clear 删除记住的密码。
func Clear(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Exists 报告是否已记住密码。
func Exists(path string) bool {
	_, ok := Load(path)
	return ok
}
