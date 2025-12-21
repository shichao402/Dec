package cmd

import (
	"fmt"
	"os"

	"github.com/shichao402/Dec/pkg/version"
	"github.com/spf13/cobra"
)

var (
	appVersion   string
	appBuildTime string
)

var RootCmd = &cobra.Command{
	Use:   "dec",
	Short: "Dec - 规则和 MCP 工具管理器",
	Long: `Dec - 规则和 MCP 工具管理器

管理 Cursor/IDE 的规则文件和 MCP 工具配置。

使用示例:
  # 更新包缓存
  dec update

  # 列出可用包
  dec list

  # 初始化项目配置
  dec init

  # 同步规则和 MCP 配置
  dec sync

  # 查看/切换包源
  dec source [url]

  # 切换版本
  dec use <version>

  # 启动 MCP Server
  dec serve`,
	Version: getVersionString(),
}

// SetVersion 设置版本信息（从编译参数注入）
func SetVersion(v, bt string) {
	appVersion = v
	appBuildTime = bt
	RootCmd.Version = getVersionString()
}

// getVersionString 获取版本字符串
func getVersionString() string {
	// 优先使用编译时注入的版本
	if appVersion != "" && appVersion != "unknown" {
		if appBuildTime != "" && appBuildTime != "unknown" {
			return fmt.Sprintf("%s (built at %s)", appVersion, appBuildTime)
		}
		return appVersion
	}

	// 如果编译时未注入，尝试从 version.json 读取
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
	// 优先使用编译时注入的版本
	if appVersion != "" && appVersion != "unknown" {
		return appVersion
	}

	// 如果编译时未注入，尝试从 version.json 读取
	workDir, err := os.Getwd()
	if err == nil {
		if ver, err := version.GetVersion(workDir); err == nil {
			return ver
		}
	}

	return "dev"
}

func init() {
	// 查询命令
	RootCmd.AddCommand(listCmd)
	// 其他命令在各自文件的 init() 中添加
}
