package app

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"

	"srvctl/internal/config"
	"srvctl/internal/sessionkey"
	"srvctl/internal/web"
)

// RunGUIOnly 强制进入界面模式，但仍解析 --portable / --vault 等全局开关。
// 供 srvctl-gui.exe 使用（双击启动，不该因为参数问题什么都不做）。
func RunGUIOnly(args []string) int {
	g, _ := parseGlobals(args)
	return RunGUI(g, true)
}

// RunGUI 启动可视化界面，返回进程退出码。
func RunGUI(g globals, open bool) (code int) {
	logger, closeLog := newLogger(g.portable)
	defer closeLog()

	defer func() {
		if r := recover(); r != nil {
			logger.Printf("panic: %v\n%s", r, debug.Stack())
			code = 1
		}
	}()

	st := openStore(g)

	// 尝试用记住的主密码自动解锁。
	// 失败也没关系 —— 界面会显示解锁页。srvctl-gui.exe 没有控制台，
	// 不可能在终端里提示输入主密码，所以解锁必须走界面。
	if st.Exists() {
		if pw, ok := sessionkey.Load(config.SessionKeyPath(g.portable)); ok {
			if err := st.Unlock(pw); err != nil {
				logger.Printf("自动解锁失败（界面将显示解锁页）: %v", err)
			}
		}
	}

	logger.Printf("%s %s 启动", appName, Version())

	if err := web.Run(web.Options{
		Store:    st,
		Portable: g.portable,
		Open:     open,
		Logger:   logger,
	}); err != nil {
		logger.Printf("界面异常退出: %v", err)
		return 1
	}
	return 0
}

// newLogger 同时写日志文件和 stderr。
//
// srvctl-gui.exe 用 -H=windowsgui 构建，没有控制台，os.Stderr 是无效句柄 ——
// 写它必然失败。所以这里**不能**用 io.MultiWriter：它碰到第一个错误就返回，
// 会把写日志文件也一起中断掉（实测症状是日志文件建出来但是 0 字节，
// GUI 出任何问题都无从排查）。用一个忽略单项错误的 tee 代替。
func newLogger(portable bool) (*log.Logger, func()) {
	path := config.LogPath(portable)

	targets := []io.Writer{}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			targets = append(targets, f)
			// stderr 放最后：控制台可用时也能看到（srvctl.exe 跑 gui 子命令的场景）
			targets = append(targets, os.Stderr)
			return log.New(teeWriter{targets}, "", log.LstdFlags), func() { _ = f.Close() }
		}
	}
	return log.New(os.Stderr, "", log.LstdFlags), func() {}
}

// teeWriter 把每次写入复制到所有目标，并忽略单个目标的错误。
type teeWriter struct{ ws []io.Writer }

func (t teeWriter) Write(p []byte) (int, error) {
	for _, w := range t.ws {
		_, _ = w.Write(p)
	}
	return len(p), nil
}
