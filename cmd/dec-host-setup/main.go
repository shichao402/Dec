package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shichao402/Dec/internal/config"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("dec-host-setup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	listen := flags.String("listen", config.ProvisionManagementListen, "dec-server 管理监听地址")
	showVersion := flags.Bool("version", false, "显示版本号")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("不接受位置参数")
	}
	if *showVersion {
		fmt.Fprintln(stdout, Version)
		return nil
	}
	if strings.TrimSpace(*listen) == "" {
		return fmt.Errorf("listen 不能为空")
	}
	result, err := config.EnsureManagementListen(*listen)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "listen=%s\n", result.Addr)
	fmt.Fprintf(stdout, "changed=%t\n", result.Changed)
	fmt.Fprintf(stdout, "config=%s\n", result.Path)
	if result.Previous != "" && result.Previous != result.Addr {
		fmt.Fprintf(stdout, "previous=%s\n", result.Previous)
	}
	fmt.Fprintln(stdout, "host-setup=ok")
	return nil
}
