//go:build !windows

package sshx

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// watchResize 在终端窗口尺寸变化时通知远端 PTY，返回停止函数。
func watchResize(fd int, sess *ssh.Session) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				if w, h, err := term.GetSize(fd); err == nil {
					_ = sess.WindowChange(h, w)
				}
			case <-done:
				return
			}
		}
	}()

	// 立刻触发一次，让远端拿到初始尺寸。
	select {
	case ch <- syscall.SIGWINCH:
	default:
	}

	return func() {
		signal.Stop(ch)
		close(done)
	}
}
