//go:build windows

package sshx

import "golang.org/x/crypto/ssh"

// watchResize 在 Windows 上是空操作。
//
// Windows 控制台没有 SIGWINCH。要跟着窗口大小走需要轮询
// GetConsoleScreenBufferInfo，v1 没做 —— 初始尺寸是正确的，
// 只是拖拽窗口后远端 PTY 不会跟着变。
func watchResize(_ int, _ *ssh.Session) func() {
	return func() {}
}
