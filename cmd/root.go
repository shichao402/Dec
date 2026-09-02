package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// devVersion 为未注入编译期版本时的统一取值，与 dec-server 的默认版本保持一致。
const devVersion = "dev"

var (
	appVersion   string
	appBuildTime string
)

var runCLIMode = executeCLI

var RootCmd = &cobra.Command{
	Use:   "dec",
	Short: "Dec - 个人 AI 知识仓库",
	Long: `Dec - 个人 AI 知识仓库

将 Skills、Rules、MCP 配置等 AI 资产保存到个人知识仓库，
跨项目、跨设备复用，效率持续积累。

人机入口是桌面管理客户端（client/），不是终端 TUI。
本程序保留版本查询与内部 hidden 命令（置备 / 新鲜度检查）。

示例:
  dec --version      # 显示版本号`,
	SilenceErrors: true,
	SilenceUsage:  true,
	Version:       getVersionString(),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("Dec 的交互入口是桌面管理客户端，终端 TUI 已移除\n\n打开 Dec Console 管理本机或远端 dec-server。\n查看版本：dec --version")
	},
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

// Execute 走最小 CLI：版本、help、内部 hidden 命令。无参不再启动 TUI。
func Execute(args []string, stdin, stdout, stderr *os.File) error {
	_ = stdin
	return runCLIMode(args, stdout, stderr)
}

func isInternalCLIArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "__freshness-check":
		return true
	case "__service-setup":
		// 置备经 SSH 调用它，非交互执行时没有 TTY。
		return true
	default:
		return false
	}
}

func executeCLI(args []string, stdout, stderr io.Writer) error {
	RootCmd.SetArgs(args)
	RootCmd.SetOut(stdout)
	RootCmd.SetErr(stderr)
	return RootCmd.Execute()
}
