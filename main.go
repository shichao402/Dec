package main

import (
	"os"

	"github.com/shichao402/Dec/cmd"
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

	// Cobra 已经打印了错误信息，这里只需要设置退出码
	if err := cmd.RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
