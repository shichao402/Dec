package main

import (
	"context"
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
	if err := decmcp.Run(context.Background(), decmcp.Config{ProjectRoot: *projectRoot}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
