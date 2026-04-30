package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/tui"
	"github.com/shichao402/Dec/pkg/freshness"
	"github.com/shichao402/Dec/pkg/update"
	"github.com/shichao402/Dec/pkg/version"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	appVersion   string
	appBuildTime string
)

type entryMode int

const (
	entryModeCLI entryMode = iota
	entryModeTUI
)

type entryContext struct {
	Args      []string
	Term      string
	NoTUI     string
	StdinTTY  bool
	StdoutTTY bool
	StderrTTY bool
}

var (
	detectTTY      = isTerminalFile
	getWorkingDir  = os.Getwd
	runCLIMode     = executeCLI
	runTUIMode     = executeTUI
	emitUpdateHint = func(w io.Writer) {
		if w == nil {
			return
		}
		if result := update.CheckBackground(GetVersion()); result != nil {
			fmt.Fprintf(w, "\n💡 %s\n\n", update.FormatUpdateHint(result))
		}
	}
	// freshnessCheckTimeout 限制整个陈旧度检查（含 git fetch）的最长阻塞时间。
	// 主命令最多等待这段时间后退出；超时则跳过提示，不影响 CLI 响应感。
	// 注意：真实网络下 `git fetch` 首次往返通常 1~3 秒，200ms 会直接超时；
	// 5 秒能让多数网络环境顺利完成。节流窗口默认 24h，单日最多触发一次阻塞。
	freshnessCheckTimeout = 5 * time.Second
	// freshnessSkipCommands 列出不应触发陈旧度检查的命令全路径。
	// - 同步类命令（pull/push）自己就是刷新入口，再触发会循环/冗余。
	// - config init 本身可能还没有 .dec/.version，也会触发首轮 fetch，打扰首次体验。
	// - update/version/help 属于元命令，不涉及资产。
	freshnessSkipCommands = []string{
		"dec pull",
		"dec push",
		"dec config init",
		"dec update",
		"dec version",
		"dec help",
	}
)

var RootCmd = &cobra.Command{
	Use:   "dec",
	Short: "Dec - 个人 AI 知识仓库",
	Long: `Dec - 个人 AI 知识仓库

将 Skills、Rules、MCP 配置等 AI 资产保存到个人知识仓库，
跨项目、跨设备复用，效率持续积累。

使用示例:
  dec config repo <url>             # 连接个人仓库
  dec config global                 # 配置本机 IDE
  dec config init                   # 初始化项目配置
  dec list                          # 列出所有 Vault 和资产
  dec search <query>                # 搜索资产
  dec pull                          # 拉取已启用资产到项目
  dec push                          # 推送修改到仓库`,
	SilenceErrors: true,
	SilenceUsage:  true,
	Version:       getVersionString(),
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if cmd.Name() == "update" || cmd.Name() == "version" {
			return
		}
		emitUpdateHint(cmd.ErrOrStderr())
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		emitFreshnessHint(cmd)
		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示当前 Dec 的版本号",
	Long: `显示当前 Dec 的版本号。

示例：
  dec version`,
	RunE: runVersion,
}

// SetVersion 设置版本信息（从编译参数注入）
func SetVersion(v, bt string) {
	appVersion = v
	appBuildTime = bt
	RootCmd.Version = getVersionString()
}

func hasInjectedVersion() bool {
	return appVersion != "" && appVersion != "unknown" && appVersion != "dev"
}

// getVersionString 获取版本字符串
func getVersionString() string {
	if hasInjectedVersion() {
		if appBuildTime != "" && appBuildTime != "unknown" {
			return fmt.Sprintf("%s (built at %s)", appVersion, appBuildTime)
		}
		return appVersion
	}

	workDir, err := os.Getwd()
	if err == nil {
		if ver, err := version.GetVersion(workDir); err == nil {
			return ver
		}
	}

	return "dev"
}

// GetVersion 获取当前版本号（供其他包使用）
func GetVersion() string {
	if hasInjectedVersion() {
		return appVersion
	}

	workDir, err := os.Getwd()
	if err == nil {
		if ver, err := version.GetVersion(workDir); err == nil {
			return ver
		}
	}

	return "dev"
}

func runVersion(cmd *cobra.Command, args []string) error {
	cmd.Println(GetVersion())
	return nil
}

// Execute 根据终端环境在 Cobra CLI 和默认 TUI 入口之间分流。
func Execute(args []string, stdin, stdout, stderr *os.File) error {
	mode := decideEntryMode(entryContext{
		Args:      append([]string(nil), args...),
		Term:      os.Getenv("TERM"),
		NoTUI:     os.Getenv("DEC_NO_TUI"),
		StdinTTY:  detectTTY(stdin),
		StdoutTTY: detectTTY(stdout),
		StderrTTY: detectTTY(stderr),
	})

	if mode == entryModeTUI {
		projectRoot, err := getWorkingDir()
		if err != nil {
			return fmt.Errorf("获取当前目录失败: %w", err)
		}
		emitUpdateHint(stderr)
		return runTUIMode(projectRoot, stdin, stdout)
	}

	return runCLIMode(args, stdout, stderr)
}

func decideEntryMode(ctx entryContext) entryMode {
	if len(ctx.Args) != 0 {
		return entryModeCLI
	}
	if strings.TrimSpace(ctx.NoTUI) == "1" {
		return entryModeCLI
	}
	if strings.EqualFold(strings.TrimSpace(ctx.Term), "dumb") {
		return entryModeCLI
	}
	if !ctx.StdinTTY || !ctx.StdoutTTY || !ctx.StderrTTY {
		return entryModeCLI
	}
	return entryModeTUI
}

func executeCLI(args []string, stdout, stderr io.Writer) error {
	RootCmd.SetArgs(args)
	RootCmd.SetOut(stdout)
	RootCmd.SetErr(stderr)
	return RootCmd.Execute()
}

func executeTUI(projectRoot string, input io.Reader, output io.Writer) error {
	return tui.Run(projectRoot, GetVersion(), input, output)
}

func isTerminalFile(file *os.File) bool {
	if file == nil {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func init() {
	RootCmd.AddCommand(versionCmd)
}

// emitFreshnessHint 在命令正常收尾后做被动的陈旧度检查。
//
// 只有命令被正常执行到 PostRun 阶段才会触发；命令执行出错、或属于 freshnessSkipCommands
// 清单里的同步/元命令时直接返回。整个检查带超时，不会拖慢 CLI 响应。
func emitFreshnessHint(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	path := cmd.CommandPath()
	for _, skip := range freshnessSkipCommands {
		if path == skip || strings.HasPrefix(path, skip+" ") {
			return
		}
	}

	projectRoot, err := getWorkingDir()
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), freshnessCheckTimeout)
	defer cancel()

	result := freshness.Check(ctx, projectRoot)
	freshness.WriteHint(cmd.ErrOrStderr(), result)
}
