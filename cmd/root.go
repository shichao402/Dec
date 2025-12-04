package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	appVersion   string
	appBuildTime string
)

var RootCmd = &cobra.Command{
	Use:   "cursortoolset",
	Short: "Cursor 工具集管理器",
	Long: `CursorToolset - Cursor 工具集管理器

一个用于管理和安装 Cursor 工具集的命令行工具。
项目根目录的 available-toolsets.json 文件定义了可用的工具集列表。
每个工具集都包含一个 toolset.json 描述文件，定义了工具的安装和配置信息。

使用示例:
  # 列出所有可用工具集
  cursortoolset list

  # 安装所有工具集
  cursortoolset install

  # 安装特定工具集
  cursortoolset install <toolset-name>

  # 清理已安装的工具集
  cursortoolset clean

  # 更新 CursorToolset 和工具集
  cursortoolset update`,
	Version: getVersionString(),
}

// SetVersion 设置版本信息
func SetVersion(v, bt string) {
	appVersion = v
	appBuildTime = bt
	RootCmd.Version = getVersionString()
}

// getVersionString 获取版本字符串
func getVersionString() string {
	if appVersion == "" {
		appVersion = "dev"
	}
	if appBuildTime != "" && appBuildTime != "unknown" {
		return fmt.Sprintf("%s (built at %s)", appVersion, appBuildTime)
	}
	return appVersion
}

// GetVersion 获取当前版本号（供其他包使用）
func GetVersion() string {
	if appVersion != "" && appVersion != "unknown" {
		return appVersion
	}
	return "dev"
}

func init() {
	RootCmd.AddCommand(installCmd)
	RootCmd.AddCommand(listCmd)
	RootCmd.AddCommand(cleanCmd)
	RootCmd.AddCommand(updateCmd)
}


