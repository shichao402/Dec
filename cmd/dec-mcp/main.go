package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	decmcp "github.com/shichao402/Dec/internal/mcp"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	projectRoot := flag.String("project-root", "", "已废弃：项目根改为每个 tool 的 project_root")
	showVersion := flag.Bool("version", false, "显示版本号")
	flag.Parse()
	if *showVersion {
		fmt.Println(Version)
		return
	}
	if strings.TrimSpace(*projectRoot) != "" {
		fmt.Fprintln(os.Stderr, "dec-mcp: --project-root 已废弃，将忽略；请在各 tool 传入 project_root")
	}
	if err := decmcp.Run(context.Background(), decmcp.Config{
		ClientVersion: Version,
	}); err != nil {
		// 信号 / 父进程退出触发的 ctx 取消属正常收尾，不作为错误。
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
