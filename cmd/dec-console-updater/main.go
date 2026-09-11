package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/shichao402/Dec/internal/sysproc"
	"github.com/shichao402/Dec/internal/update"
)

func main() {
	if len(os.Args) < 2 {
		fail("缺少命令")
	}
	switch os.Args[1] {
	case "check":
		runCheck(os.Args[2:])
	case "download":
		runDownload(os.Args[2:])
	case "apply":
		runApply(os.Args[2:])
	default:
		fail("未知命令 %q", os.Args[1])
	}
}

func commonFlags(name string, args []string) (*flag.FlagSet, *string, *string) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	current := flags.String("current", "", "current Console version")
	dataDir := flags.String("data-dir", "", "Console updater data directory")
	if err := flags.Parse(args); err != nil {
		fail("%v", err)
	}
	if *current == "" || *dataDir == "" {
		fail("--current 和 --data-dir 不能为空")
	}
	return flags, current, dataDir
}

func runCheck(args []string) {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	current := flags.String("current", "", "current Console version")
	dataDir := flags.String("data-dir", "", "Console updater data directory")
	force := flags.Bool("force", false, "ignore check throttle")
	if err := flags.Parse(args); err != nil {
		fail("%v", err)
	}
	if *current == "" || *dataDir == "" {
		fail("--current 和 --data-dir 不能为空")
	}
	result, err := update.CheckConsoleUpdate(context.Background(), *current, *dataDir, *force)
	if err != nil {
		fail("%v", err)
	}
	writeJSON(result)
}

func runDownload(args []string) {
	_, current, dataDir := commonFlags("download", args)
	path, status, err := update.DownloadConsoleUpdate(context.Background(), *current, *dataDir)
	if err != nil {
		fail("%v", err)
	}
	writeJSON(struct {
		PackagePath string                     `json:"packagePath"`
		Status      *update.ConsoleCheckResult `json:"status"`
	}{PackagePath: path, Status: status})
}

func runApply(args []string) {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	pkg := flags.String("package", "", "verified Console installer")
	parent := flags.Int("parent-pid", 0, "Console process to allow time to exit")
	relaunch := flags.String("relaunch", "", "installed Console executable")
	if err := flags.Parse(args); err != nil {
		fail("%v", err)
	}
	if *pkg == "" {
		fail("--package 不能为空")
	}
	if *parent > 0 {
		time.Sleep(1500 * time.Millisecond)
	}
	switch runtime.GOOS {
	case "windows":
		cmd := sysproc.Command(*pkg, "/S")
		if err := cmd.Run(); err != nil {
			fail("静默安装失败: %v", err)
		}
		if *relaunch != "" {
			_ = sysproc.Command(*relaunch).Start()
		}
	case "darwin":
		if err := sysproc.Command("open", *pkg).Start(); err != nil {
			fail("打开安装包失败: %v", err)
		}
	default:
		if err := sysproc.Command(*pkg).Start(); err != nil {
			fail("打开安装包失败: %v", err)
		}
	}
}

func writeJSON(value any) {
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
