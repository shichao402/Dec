package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "同步更新已有包项目的配置",
	Long: `同步更新已有包项目的配置到最新版本。

此命令用于同步包项目中的配置信息，例如迁移旧版本配置格式等。

注意：包开发文档和规则现已通过 CursorColdStart 的 dec pack 提供，
请使用 coldstart enable dec 获取完整的开发指南。

必须在包项目根目录（包含 package.json）下执行。

示例：
  dec sync`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 检查当前目录是否是一个包项目
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("获取当前目录失败: %w", err)
		}

		packageJSONPath := filepath.Join(cwd, "package.json")
		if _, err := os.Stat(packageJSONPath); os.IsNotExist(err) {
			return fmt.Errorf("当前目录不是 Dec 包项目（未找到 package.json）\n\n提示: 请在包项目根目录下执行此命令")
		}

		// 读取 package.json 获取包名
		packageName, err := getPackageNameFromJSON(packageJSONPath)
		if err != nil {
			return fmt.Errorf("读取 package.json 失败: %w", err)
		}

		fmt.Printf("🔄 同步包项目: %s\n\n", packageName)

		// TODO: 添加具体的同步逻辑（配置迁移等）
		fmt.Println("ℹ️  当前没有需要同步的配置")
		fmt.Println("\n💡 提示：包开发文档和规则现已通过 CursorColdStart 提供")
		fmt.Println("   请运行: coldstart enable dec && coldstart init .")

		return nil
	},
}

func init() {
	RootCmd.AddCommand(syncCmd)
}

// getPackageNameFromJSON 从 package.json 读取包名
func getPackageNameFromJSON(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var pkg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", err
	}

	if pkg.Name == "" {
		return "", fmt.Errorf("package.json 中缺少 name 字段")
	}

	return pkg.Name, nil
}
