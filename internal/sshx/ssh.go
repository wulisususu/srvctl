// Package sshx 封装 SSH 连接。
//
// 两个来自 Conduit 的踩坑经验直接体现在这里：
//
//  1. 密码认证必须同时注册 keyboard-interactive
//     （electron/services/ssh/client.ts:113-135）。很多设备 —— ESXi、网络
//     设备、部分交换机 —— 只广告 keyboard-interactive 而不提供 plain
//     password，不注册这个回调就会报 "unable to authenticate"。
//
//  2. 私钥要先做 CRLF → LF 归一化（client.ts:140）。Windows 上复制粘贴的
//     私钥几乎必然带 \r\n，直接交给解析器会失败。
package sshx

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"srvctl/internal/model"
)

// DialTimeout 是建立 SSH 连接的超时。
const DialTimeout = 15 * time.Second

// Result 是一次远程命令执行的结果。
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func clientConfig(s model.Server) (*ssh.ClientConfig, error) {
	var auths []ssh.AuthMethod

	switch s.AuthMode {
	case model.AuthPassword:
		auths = append(auths,
			ssh.Password(s.Password),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = s.Password
				}
				return answers, nil
			}),
		)
	default:
		signer, err := parseKey(s)
		if err != nil {
			return nil, err
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}

	if s.Username == "" {
		return nil, fmt.Errorf("服务器 %s 未设置用户名", s.Name)
	}

	return &ssh.ClientConfig{
		User: s.Username,
		Auth: auths,
		// 首次连接信任任何主机密钥（TOFU 的简化版）。
		// 已知代价：不做 MITM 防护。后续可以记录 fingerprint 并在变化时告警。
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         DialTimeout,
	}, nil
}

func parseKey(s model.Server) (ssh.Signer, error) {
	key := decodePrivateKey(s.PrivateKey)
	if s.Passphrase != "" {
		signer, err := ssh.ParsePrivateKeyWithPassphrase(key, []byte(s.Passphrase))
		if err != nil {
			return nil, fmt.Errorf("私钥解析失败（口令是否正确？）: %w", err)
		}
		return signer, nil
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		if strings.Contains(err.Error(), "encrypted") {
			return nil, fmt.Errorf("私钥已加密，请在 Passphrase 字段填写口令")
		}
		return nil, fmt.Errorf("私钥解析失败: %w", err)
	}
	return signer, nil
}

// decodePrivateKey 归一化换行。Windows 上粘贴的私钥常带 \r\n。
func decodePrivateKey(s string) []byte {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return []byte(s)
}

func dial(s model.Server) (*ssh.Client, error) {
	cfg, err := clientConfig(s)
	if err != nil {
		return nil, err
	}
	return ssh.Dial("tcp", s.SSHAddress(), cfg)
}

// Run 在目标主机上执行一条命令。
//
// 命令返回非零退出码不算"连接失败" —— exit code 会透传，err 为 nil，
// 这样 AI 能区分"连不上"和"命令正常执行但失败了"。
func Run(s model.Server, command string) (Result, error) {
	client, err := dial(s)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	defer sess.Close()

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	runErr := sess.Run(command)
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}

	if runErr == nil {
		return res, nil
	}

	var exitErr *ssh.ExitError
	if errors.As(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitStatus()
		return res, nil
	}

	res.ExitCode = -1
	return res, runErr
}
