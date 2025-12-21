package cmd

import (
	"fmt"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/packages"
	"github.com/spf13/cobra"
)

var (
	listType string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有可用的包",
	Long: `列出所有可用的规则和 MCP。

支持按类型过滤：
  dec list              # 列出所有包
  dec list --type rule  # 只列出规则
  dec list --type mcp   # 只列出 MCP

如果没有可用的包，请先运行 'dec update' 更新包缓存。`,
	RunE: runList,
}

func init() {
	listCmd.Flags().StringVar(&listType, "type", "", "按类型过滤 (rule, mcp)")
}

func runList(cmd *cobra.Command, args []string) error {
	// 创建扫描器
	scanner, err := config.NewScanner()
	if err != nil {
		return fmt.Errorf("创建扫描器失败: %w", err)
	}

	// 检查是否有可用的包
	if !scanner.HasPackages() {
		fmt.Println("📦 没有可用的包")
		fmt.Println()
		fmt.Println("请先运行 'dec update' 更新包缓存")
		return nil
	}

	// 显示当前配置
	cfg, err := config.LoadGlobalConfig()
	if err == nil {
		fmt.Printf("📦 包版本: %s\n\n", cfg.PackagesVersion)
	}

	// 根据类型过滤显示
	if listType == "" || listType == "rule" {
		if err := listRules(scanner); err != nil {
			return err
		}
	}

	if listType == "" || listType == "mcp" {
		if listType == "" {
			fmt.Println()
		}
		if err := listMCPs(scanner); err != nil {
			return err
		}
	}

	return nil
}

func listRules(scanner *packages.Scanner) error {
	rules, err := scanner.ScanRules()
	if err != nil {
		return fmt.Errorf("扫描规则失败: %w", err)
	}

	if len(rules) == 0 {
		fmt.Println("📜 没有可用的规则")
		return nil
	}

	// 按分类组织规则
	categoryRules := make(map[string][]packages.RuleInfo)
	for _, rule := range rules {
		categoryRules[rule.Category] = append(categoryRules[rule.Category], rule)
	}

	fmt.Printf("📜 规则 (%d 个):\n", len(rules))

	// 按顺序显示：先显示保留分类，再显示扩展分类
	displayOrder := append([]string{}, packages.ReservedCategories...)
	for cat := range categoryRules {
		isReserved := false
		for _, reserved := range packages.ReservedCategories {
			if cat == reserved {
				isReserved = true
				break
			}
		}
		if !isReserved {
			displayOrder = append(displayOrder, cat)
		}
	}

	for _, category := range displayOrder {
		rules, ok := categoryRules[category]
		if !ok || len(rules) == 0 {
			continue
		}

		// 分类标题
		categoryLabel := getCategoryLabel(category)
		fmt.Printf("\n  [%s] %s:\n", category, categoryLabel)

		for _, rule := range rules {
			fmt.Printf("    - %s", rule.Name)
			if rule.Description != "" {
				fmt.Printf(" - %s", rule.Description)
			}
			fmt.Println()
		}
	}

	return nil
}

func listMCPs(scanner *packages.Scanner) error {
	mcps, err := scanner.ScanMCPs()
	if err != nil {
		return fmt.Errorf("扫描 MCP 失败: %w", err)
	}

	if len(mcps) == 0 {
		fmt.Println("🔧 没有可用的 MCP")
		return nil
	}

	fmt.Printf("🔧 MCP (%d 个):\n\n", len(mcps))

	for _, mcp := range mcps {
		fmt.Printf("  - %s", mcp.Name)
		if mcp.Description != "" {
			fmt.Printf(" - %s", mcp.Description)
		}
		fmt.Println()
	}

	return nil
}

// getCategoryLabel 获取分类的显示标签
func getCategoryLabel(category string) string {
	labels := map[string]string{
		"core":       "核心规则（总是注入）",
		"languages":  "编程语言",
		"frameworks": "框架",
		"platforms":  "目标平台",
		"patterns":   "设计模式",
	}

	if label, ok := labels[category]; ok {
		return label
	}

	// 扩展分类
	if len(category) > 4 && category[:4] == "ext-" {
		return "扩展: " + category[4:]
	}

	return "自定义"
}
