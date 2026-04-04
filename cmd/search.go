package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "搜索资产",
	Long: `在仓库中搜索资产。

当前实现按资产名称匹配。

示例：
  dec search "API"
  dec search "test"`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := strings.ToLower(args[0])

	return withReadRepoDir(func(repoDir string) error {
		folders, err := readFolderEntries(repoDir)
		if err != nil {
			return fmt.Errorf("读取仓库目录失败: %w", err)
		}

		var results []repoAssetInfo
		for _, f := range folders {
			assets := listFolderAssets(f.path, f.name)
			for _, a := range assets {
				if strings.Contains(strings.ToLower(a.Name), query) {
					results = append(results, a)
				}
			}
		}

		if len(results) == 0 {
			fmt.Printf("未找到匹配 \"%s\" 的资产\n", args[0])
			return nil
		}

		fmt.Printf("🔍 搜索 \"%s\"，找到 %d 个结果:\n\n", args[0], len(results))
		for _, r := range results {
			fmt.Printf("  [%s] %-24s  (vault: %s)\n", r.Type, r.Name, r.Vault)
		}

		return nil
	})
}

func init() {
	RootCmd.AddCommand(searchCmd)
}
