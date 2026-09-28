// Command srvctl-gui 是无窗口版本的入口 —— 双击直接开界面，不弹控制台黑框。
//
// 必须用 -ldflags "-H=windowsgui" 构建（见 build.ps1），否则 Windows 会为它
// 分配一个控制台窗口。所有日志写入 %APPDATA%\srvctl\srvctl.log。
package main

import (
	"os"

	"srvctl/internal/app"
)

func main() {
	os.Exit(app.RunGUIOnly(os.Args[1:]))
}
