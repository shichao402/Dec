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
	Short: "Dec - 个人 AI 知识仓库",
	Long: `Dec - 个人 AI 知识仓库

将 Skills、Rules、MCP 配置等 AI 资产保存到个人知识仓库，
跨项目、跨设备复用，效率持续积累。

使用示例:
  dec init                          # 初始化项目配置
  dec vault init --repo <url>       # 初始化个人知识仓库
  dec vault save skill <path>       # 保存 Skill 到仓库
  dec vault find "API test"         # 搜索仓库中的资产
  dec vault pull skill my-skill     # 下载 Skill 到项目
  dec sync                          # 按配置批量同步资产到 IDE`,
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
	if appVersion != "" && appVersion != "unknown" {
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
	if appVersion != "" && appVersion != "unknown" {
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
