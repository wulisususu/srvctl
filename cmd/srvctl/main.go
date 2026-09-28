// Command srvctl 是控制台版本的入口 —— 给命令行和 AI 使用。
package main

import (
	"os"

	"srvctl/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:]))
}
