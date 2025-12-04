package main

import (
	"fmt"
	"os"

	"github.com/firoyang/CursorToolset/cmd"
)

var (
	// Version 版本号（编译时注入）
	Version = "dev"
	// BuildTime 构建时间（编译时注入）
	BuildTime = "unknown"
)

func main() {
	// 设置版本信息
	cmd.SetVersion(Version, BuildTime)
	
	if err := cmd.RootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}


