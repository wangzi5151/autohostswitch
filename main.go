// Command autohostswitch 是纯本地、零网络的跨平台 hosts 管理切换工具。
//
// 单二进制、无运行时依赖；Web UI 内嵌在程序里，只监听 127.0.0.1。
package main

import (
	"os"

	"github.com/wangzi5151/autohostswitch/cli"
)

// version 由构建脚本通过 -ldflags 注入，默认 dev。
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version))
}
