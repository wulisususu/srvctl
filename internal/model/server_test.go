package model

import (
	"strings"
	"testing"
)

func base(platform string) Server {
	return Server{
		Name: "x", Host: "1.2.3.4", Platform: platform,
		Username: "u", AuthMode: AuthPassword, Password: "p",
	}
}

// 端口留空表示"用平台默认值"，而不是"22"。
// 一台 Windows 服务器显示成 1.2.3.4:22 是错的 —— 它默认走 RDP 3389。
func TestEffectivePortIsPlatformAware(t *testing.T) {
	cases := []struct {
		name     string
		srv      Server
		wantPort int
		wantAddr string
	}{
		{"Linux 留空", base(PlatformLinux), 22, "1.2.3.4:22"},
		{"Windows 留空", base(PlatformWindows), 3389, "1.2.3.4:3389"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.srv.EffectivePort(); got != c.wantPort {
				t.Errorf("EffectivePort() = %d, want %d", got, c.wantPort)
			}
			if got := c.srv.Address(); got != c.wantAddr {
				t.Errorf("Address() = %q, want %q", got, c.wantAddr)
			}
		})
	}
}

// 显式端口永远优先于平台默认，两个平台都是。
func TestExplicitPortWins(t *testing.T) {
	for _, p := range []string{PlatformLinux, PlatformWindows} {
		s := base(p)
		s.Port = 2222
		if got := s.EffectivePort(); got != 2222 {
			t.Errorf("%s: EffectivePort() = %d, want 2222", p, got)
		}
	}
}

// SSH 的默认端口永远是 22，不跟着平台走。
//
// 这是 EffectivePort 和 SSHPort 分开的原因：执行命令走 SSH，如果 Windows
// 条目拿去用 EffectivePort，就会去连 3389 上的 SSH —— 必然失败。
func TestSSHPortIgnoresPlatform(t *testing.T) {
	win := base(PlatformWindows)
	if got := win.SSHPort(); got != 22 {
		t.Errorf("Windows 条目的 SSHPort() = %d, want 22", got)
	}
	if got := win.SSHAddress(); got != "1.2.3.4:22" {
		t.Errorf("SSHAddress() = %q, want 1.2.3.4:22", got)
	}
	// 和 Address() 必须不同，否则就是这个区分失效了
	if win.Address() == win.SSHAddress() {
		t.Error("Windows 条目的 Address() 与 SSHAddress() 不该相同")
	}

	win.Port = 2200
	if got := win.SSHPort(); got != 2200 {
		t.Errorf("显式端口应生效，得到 %d", got)
	}
}

// Windows 条目默认只记录不检测。
func TestWindowsDefaultsToNoCheck(t *testing.T) {
	s := base(PlatformWindows)
	s.Normalize()
	if s.CheckMode != CheckNone {
		t.Errorf("Windows 条目默认应为 %q，得到 %q", CheckNone, s.CheckMode)
	}

	l := base(PlatformLinux)
	l.Normalize()
	if l.CheckMode != CheckAuto {
		t.Errorf("Linux 条目默认应为 %q，得到 %q", CheckAuto, l.CheckMode)
	}
}

// 备注尾部空白要去掉 —— 界面上删掉最后一行常留下空行，
// 不清掉会一路带进「复制给 AI」的文本里。
func TestNormalizeTrimsNotesTrailingWhitespace(t *testing.T) {
	cases := []struct{ in, want string }{
		{"RDP 连接时勾选「允许剪贴板」\n", "RDP 连接时勾选「允许剪贴板」"},
		{"第一行\n第二行\n\n", "第一行\n第二行"},
		{"  前后都有空格  ", "  前后都有空格"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		s := base(PlatformLinux)
		s.Notes = c.in
		s.Normalize()
		if s.Notes != c.want {
			t.Errorf("Normalize(%q) 备注 = %q, want %q", c.in, s.Notes, c.want)
		}
	}
}

// 行首缩进要保留 —— 用户可能有意排版（比如列表项）。
func TestNormalizeKeepsNotesLeadingIndent(t *testing.T) {
	s := base(PlatformLinux)
	s.Notes = "  缩进的一行\n  另一行"
	s.Normalize()
	if !strings.HasPrefix(s.Notes, "  缩进的一行") {
		t.Errorf("行首缩进被吃掉了: %q", s.Notes)
	}
}
