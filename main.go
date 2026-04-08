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

	if err := cmd.RootCmd.Execute(); err != nil {
		cmd.PrintCommandError(os.Stderr, os.Args[1:], err)
		os.Exit(1)
	}
}
