package sshx

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"srvctl/internal/model"
)

// Shell 打开一个交互式远程 shell，并把它接到当前终端上。
func Shell(s model.Server) error {
	client, err := dial(s)
	if err != nil {
		return err
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("切换到原始终端模式失败: %w", err)
		}
		defer func() { _ = term.Restore(fd, oldState) }()

		w, h, err := term.GetSize(fd)
		if err != nil || w == 0 || h == 0 {
			w, h = 80, 24
		}

		modes := ssh.TerminalModes{
			ssh.ECHO:          1,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		if err := sess.RequestPty("xterm-256color", h, w, modes); err != nil {
			return fmt.Errorf("申请 PTY 失败: %w", err)
		}

		stop := watchResize(fd, sess)
		defer stop()
	}

	sess.Stdin = os.Stdin
	sess.Stdout = os.Stdout
	sess.Stderr = os.Stderr

	if err := sess.Shell(); err != nil {
		return fmt.Errorf("启动远程 shell 失败: %w", err)
	}

	// 交互式 shell 退出时远端通常返回非零，这不算错误。
	if err := sess.Wait(); err != nil {
		var exitErr *ssh.ExitError
		if !errors.As(err, &exitErr) {
			return err
		}
	}
	return nil
}
