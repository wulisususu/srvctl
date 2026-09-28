// Package model 定义服务器的数据模型。
//
// 注意：没有独立的 Server / Connection 两张表。一条记录就是一条记录，
// 运行中的会话不落盘 —— 这是与 Conduit 最大的结构差异，也是"轻量"的来源。
package model

import (
	"fmt"
	"strings"
)

// 平台。
const (
	PlatformLinux   = "linux"
	PlatformWindows = "windows"
)

// 认证方式。
const (
	AuthPassword = "password"
	AuthKey      = "key"
)

// 连通性检测策略。
const (
	// CheckAuto 参与 TCP 探测，产出 绿/红/灰。
	CheckAuto = "auto"
	// CheckNone 只记录不检测，UI 显示 ➖（Windows 条目默认值）。
	CheckNone = "none"
)

// Server 是一条服务器记录。
//
// Password / PrivateKey / Passphrase 随 vault 整体加密后落盘。
type Server struct {
	Name         string   `json:"name"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Platform     string   `json:"platform"`
	Username     string   `json:"username"`
	AuthMode     string   `json:"auth_mode"`
	Password     string   `json:"password,omitempty"`
	PrivateKey   string   `json:"private_key,omitempty"`
	Passphrase   string   `json:"passphrase,omitempty"`
	Category     string   `json:"category,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	RequiredSSID string   `json:"required_ssid,omitempty"`
	CheckMode    string   `json:"check_mode"`
	Notes        string   `json:"notes,omitempty"`
}

// EffectivePort 返回探测/连接实际使用的端口。
func (s Server) EffectivePort() int {
	if s.Port > 0 {
		return s.Port
	}
	return 22
}

// Checkable 决定该条目是否进入探测队列。
func (s Server) Checkable() bool {
	return s.CheckMode != CheckNone && s.Host != ""
}

// Address 返回 host:port。
func (s Server) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.EffectivePort())
}

// Normalize 填充默认值。
func (s *Server) Normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.Host = strings.TrimSpace(s.Host)
	s.Username = strings.TrimSpace(s.Username)
	s.Category = strings.TrimSpace(s.Category)
	s.RequiredSSID = strings.TrimSpace(s.RequiredSSID)

	if s.Platform == "" {
		s.Platform = PlatformLinux
	}
	if s.AuthMode == "" {
		if strings.TrimSpace(s.PrivateKey) != "" {
			s.AuthMode = AuthKey
		} else {
			s.AuthMode = AuthPassword
		}
	}
	if s.CheckMode == "" {
		// Windows 条目默认只记录不检测。
		if s.Platform == PlatformWindows {
			s.CheckMode = CheckNone
		} else {
			s.CheckMode = CheckAuto
		}
	}
	if s.Port < 0 {
		s.Port = 0
	}
}

// Validate 校验必填项与取值合法性。
func (s Server) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("名称不能为空")
	}
	if s.Host == "" {
		return fmt.Errorf("主机地址不能为空")
	}
	switch s.Platform {
	case PlatformLinux, PlatformWindows:
	default:
		return fmt.Errorf("未知平台: %q（应为 %s 或 %s）", s.Platform, PlatformLinux, PlatformWindows)
	}
	switch s.AuthMode {
	case AuthPassword:
		if s.Password == "" {
			return fmt.Errorf("认证方式为密码，但密码为空")
		}
	case AuthKey:
		if strings.TrimSpace(s.PrivateKey) == "" {
			return fmt.Errorf("认证方式为密钥，但私钥为空")
		}
	default:
		return fmt.Errorf("未知认证方式: %q（应为 %s 或 %s）", s.AuthMode, AuthPassword, AuthKey)
	}
	return nil
}
