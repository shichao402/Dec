package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	decmcp "github.com/shichao402/Dec/internal/mcp"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	projectRoot := flag.String("project-root", "", "项目根目录（默认 DEC_PROJECT_ROOT 或当前目录）")
	flag.Parse()
	if err := decmcp.Run(context.Background(), decmcp.Config{
		ProjectRoot:   *projectRoot,
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
