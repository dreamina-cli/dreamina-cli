package main

// 该文件已按当前工程布局整理为可编译源码，并保留必要的恢复说明。

import (
	"fmt"
	"os"

	"code.byted.org/videocut-aigc/dreamina_cli/cmd"
	"code.byted.org/videocut-aigc/dreamina_cli/infra/envsetup"
)

// main 是 CLI 程序入口：先准备运行环境，再执行命令并把错误写到 stderr。
func main() {
	// 当前主流程为：
	// call cmd.ExecuteArgs(os.Args[1:]), print error to stderr, then exit 1.
	if err := envsetup.Apply(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := cmd.ExecuteArgs(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
