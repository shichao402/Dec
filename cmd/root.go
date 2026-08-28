package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/serviceapi"
	"github.com/shichao402/Dec/internal/tui"
	"github.com/shichao402/Dec/internal/update"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// devVersion 为未注入编译期版本时的统一取值，与 dec-server 的默认版本保持一致。
const devVersion = "dev"

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
	detectTTY           = isTerminalFile
	getWorkingDir       = os.Getwd
	runCLIMode          = executeCLI
	runTUIMode          = executeTUI
	runTUIWorkspaceMode = executeTUIWorkspace
	emitUpdateHint      = func(w io.Writer) {
		if w == nil {
			return
		}
		if result := update.CheckBackground(GetVersion()); result != nil {
			fmt.Fprintf(w, "\n💡 %s\n\n", update.FormatUpdateHint(result))
		}
	}
)

var RootCmd = &cobra.Command{
	Use:   "dec",
	Short: "Dec - 个人 AI 知识仓库",
	Long: `Dec - 个人 AI 知识仓库

将 Skills、Rules、MCP 配置等 AI 资产保存到个人知识仓库，
跨项目、跨设备复用，效率持续积累。

在交互式终端中直接运行 dec 即可进入 TUI Shell。

示例:
  dec                # 启动 TUI（本仓库）
  dec --global       # 启动本机平面 TUI
  dec --version      # 显示版本号

自更新：打开 TUI → 同步页按 u`,
	SilenceErrors: true,
	SilenceUsage:  true,
	Version:       getVersionString(),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("Dec 需要交互式终端\n\n在 TTY 中运行 dec 启动 TUI，或使用 dec --version 查看版本")
	},
}

func init() {
	RootCmd.PersistentFlags().Bool("global", false, "启动本机平面 TUI")
	RootCmd.PersistentFlags().Bool("user", false, "启动本机平面 TUI（--global 的别名）")
	_ = RootCmd.PersistentFlags().MarkHidden("user")
}

// SetVersion 设置版本信息（从编译参数注入）
func SetVersion(v, bt string) {
	appVersion = v
	appBuildTime = bt
	RootCmd.Version = getVersionString()
}

func hasInjectedVersion() bool {
	return appVersion != "" && appVersion != "unknown" && appVersion != devVersion
}

// getVersionString 获取版本字符串
func getVersionString() string {
	if hasInjectedVersion() {
		if appBuildTime != "" && appBuildTime != "unknown" {
			return fmt.Sprintf("%s (built at %s)", appVersion, appBuildTime)
		}
		return appVersion
	}

	return devVersion
}

// GetVersion 获取当前版本号（供其他包使用）。
// 未注入版本时一律为 dev：不得回退到工作目录的 version.json——那可能是任意项目的
// 版本号，会与恒为 dev 的 dec-server 误判成版本不一致。
func GetVersion() string {
	if hasInjectedVersion() {
		return appVersion
	}

	return devVersion
}

// Execute 根据终端环境在最小 CLI 与默认 TUI 入口之间分流。
func Execute(args []string, stdin, stdout, stderr *os.File) error {
	if isInternalCLIArgs(args) {
		return runCLIMode(args, stdout, stderr)
	}

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
		if isUserTUIArgs(args) {
			return runTUIWorkspaceMode(app.NewWorkspace(app.WorkspaceUser, ""), stdin, stdout)
		}
		return runTUIMode(projectRoot, stdin, stdout)
	}

	return runCLIMode(args, stdout, stderr)
}

func isInternalCLIArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "__freshness-check":
		return true
	default:
		return false
	}
}

func decideEntryMode(ctx entryContext) entryMode {
	if len(ctx.Args) != 0 && !isUserTUIArgs(ctx.Args) {
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

func isUserTUIArgs(args []string) bool {
	if len(args) != 1 {
		return false
	}
	return args[0] == "--user" || args[0] == "--global"
}

func executeCLI(args []string, stdout, stderr io.Writer) error {
	RootCmd.SetArgs(args)
	RootCmd.SetOut(stdout)
	RootCmd.SetErr(stderr)
	return RootCmd.Execute()
}

func executeTUI(projectRoot string, input io.Reader, output io.Writer) error {
	return executeTUIWorkspace(app.NewWorkspace(app.WorkspaceProject, projectRoot), input, output)
}

func executeTUIWorkspace(workspace app.Workspace, input io.Reader, output io.Writer) error {
	api, err := serviceapi.Connect(context.Background(), "tui", fmt.Sprintf("tui-%d", os.Getpid()), GetVersion())
	if err != nil {
		return fmt.Errorf("连接 dec-server 失败: %w", err)
	}
	defer api.Close()
	serviceapi.SetDefault(api)
	return tui.RunWithOptions(workspace.Root, GetVersion(), tui.RunOptions{Plane: workspace.EffectivePlane()}, input, output)
}

func isTerminalFile(file *os.File) bool {
	if file == nil {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}
