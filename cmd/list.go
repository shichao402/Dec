package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有资产",
	Long: `列出当前仓库中的所有资产。

示例：
  dec list`,
	RunE: runList,
}

func runList(cmd *cobra.Command, args []string) error {
	return withReadRepoDir(func(repoDir string) error {
		folders, err := readFolderEntries(repoDir)
		if err != nil {
			return fmt.Errorf("读取仓库目录失败: %w", err)
		}

		if len(folders) == 0 {
			fmt.Println("仓库中还没有资产")
			return nil
		}

		fmt.Printf("📦 仓库资产 (%d 个目录):\n", len(folders))
		for _, f := range folders {
			assets := listFolderAssets(f.path, f.name)

			skillCount, ruleCount, mcpCount := 0, 0, 0
			for _, a := range assets {
				switch a.Type {
				case "skill":
					skillCount++
				case "rule":
					ruleCount++
				case "mcp":
					mcpCount++
				}
			}
			var parts []string
			if skillCount > 0 {
				parts = append(parts, fmt.Sprintf("%d skills", skillCount))
			}
			if ruleCount > 0 {
				parts = append(parts, fmt.Sprintf("%d rules", ruleCount))
			}
			if mcpCount > 0 {
				parts = append(parts, fmt.Sprintf("%d mcps", mcpCount))
			}
			summary := "(空)"
			if len(parts) > 0 {
				summary = strings.Join(parts, ", ")
			}
			fmt.Printf("\n  %s/  (%s)\n", f.name, summary)

			for _, a := range assets {
				fmt.Printf("    [%-5s] %s\n", a.Type, a.Name)
			}
		}

		return nil
	})
}

func init() {
	RootCmd.AddCommand(listCmd)
}
