package cmd

import (
	"fmt"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "管理配置",
	Long:  `查看和修改 Dec 配置。`,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "获取配置项",
	Long: `获取指定配置项的值。

支持的配置项：
  registry_url  - Registry 源地址`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		switch key {
		case "registry_url":
			fmt.Println(config.GetRegistryURL())
		default:
			fmt.Printf("❌ 未知配置项: %s\n", key)
			fmt.Println("支持的配置项: registry_url")
		}
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "设置配置项",
	Long: `设置指定配置项的值。

支持的配置项：
  registry_url  - Registry 源地址

示例：
  dec config set registry_url https://mirror.example.com/registry.json`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		value := args[1]

		switch key {
		case "registry_url":
			if err := config.SetRegistryURL(value); err != nil {
				fmt.Printf("❌ 设置失败: %v\n", err)
				return
			}
			fmt.Printf("✅ registry_url 已设置为: %s\n", value)
		default:
			fmt.Printf("❌ 未知配置项: %s\n", key)
			fmt.Println("支持的配置项: registry_url")
		}
	},
}

var configResetCmd = &cobra.Command{
	Use:   "reset <key>",
	Short: "重置配置项为默认值",
	Long: `重置指定配置项为默认值。

支持的配置项：
  registry_url  - Registry 源地址`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		switch key {
		case "registry_url":
			if err := config.ResetRegistryURL(); err != nil {
				fmt.Printf("❌ 重置失败: %v\n", err)
				return
			}
			fmt.Printf("✅ registry_url 已重置为默认值: %s\n", config.GetDefaultRegistryURL())
		default:
			fmt.Printf("❌ 未知配置项: %s\n", key)
			fmt.Println("支持的配置项: registry_url")
		}
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有配置",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.Get()
		fmt.Println("当前配置:")
		fmt.Println()

		// Registry URL
		registryURL := config.GetRegistryURL()
		if cfg.RegistryURL != "" {
			fmt.Printf("  registry_url = %s\n", registryURL)
		} else {
			fmt.Printf("  registry_url = %s (默认)\n", registryURL)
		}

		fmt.Println()
		fmt.Println("配置文件位置:")
		if path, err := config.GetConfigPath(); err == nil {
			fmt.Printf("  %s\n", path)
		}

		fmt.Println()
		fmt.Println("环境变量:")
		fmt.Println("  DEC_HOME     - 自定义安装目录")
		fmt.Println("  DEC_REGISTRY - 自定义 Registry URL（优先级最高）")
	},
}

func init() {
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configResetCmd)
	configCmd.AddCommand(configListCmd)
	RootCmd.AddCommand(configCmd)
}
