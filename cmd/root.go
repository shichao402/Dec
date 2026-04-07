package cmd

import (
	"fmt"
	"os"

	"github.com/shichao402/Dec/pkg/update"
	"github.com/shichao402/Dec/pkg/version"
	"github.com/spf13/cobra"
)

var (
	appVersion   string
	appBuildTime string
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
	Version: getVersionString(),
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// 跳过 update 和 version 命令自身的检查
		if cmd.Name() == "update" || cmd.Name() == "version" {
			return
		}
		// 后台检查新版本
		if result := update.CheckBackground(GetVersion()); result != nil {
			fmt.Fprintf(os.Stderr, "\n💡 %s\n\n", update.FormatUpdateHint(result))
		}
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

func init() {
	RootCmd.AddCommand(versionCmd)
}
