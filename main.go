package main

import (
	"os"

	"github.com/shichao402/Dec/cmd"
)

var (
	// Version 版本号（编译时用 -X main.Version 注入）
	Version = "dev"
	// BuildTime 保留给旧二进制展示；发版不再 -X 注入，避免污染产物哈希。
	BuildTime = "unknown"
)

func main() {
	// 设置版本信息
	cmd.SetVersion(Version, BuildTime)

	if err := cmd.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		cmd.PrintCommandError(os.Stderr, os.Args[1:], err)
		os.Exit(1)
	}
}
