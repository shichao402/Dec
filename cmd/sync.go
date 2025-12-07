package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/spf13/cobra"
)

var (
	syncAll      bool
	syncWorkflow bool
	syncGuide    bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "同步更新已有包项目的开发文件",
	Long: `同步更新已有包项目的开发文件到最新版本。

此命令用于更新已有 CursorToolset 包项目中的：
  - .cursortoolset/docs/package-dev-guide.md  包开发指南
  - .github/workflows/release.yml             发布工作流

必须在包项目根目录（包含 package.json）下执行。

示例：
  # 同步所有文件（默认）
  cursortoolset sync

  # 仅同步开发指南
  cursortoolset sync --guide

  # 仅同步 workflow
  cursortoolset sync --workflow`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 检查当前目录是否是一个包项目
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("获取当前目录失败: %w", err)
		}

		packageJSONPath := filepath.Join(cwd, "package.json")
		if _, err := os.Stat(packageJSONPath); os.IsNotExist(err) {
			return fmt.Errorf("当前目录不是 CursorToolset 包项目（未找到 package.json）\n\n提示: 请在包项目根目录下执行此命令")
		}

		// 读取 package.json 获取包名
		packageName, err := getPackageNameFromJSON(packageJSONPath)
		if err != nil {
			return fmt.Errorf("读取 package.json 失败: %w", err)
		}

		fmt.Printf("🔄 同步包项目: %s\n\n", packageName)

		// 如果没有指定任何选项，默认同步所有
		if !syncWorkflow && !syncGuide {
			syncAll = true
		}

		syncedCount := 0

		// 同步开发指南
		if syncAll || syncGuide {
			if err := syncPackageDevGuide(cwd); err != nil {
				fmt.Printf("  ⚠️  同步开发指南失败: %v\n", err)
			} else {
				fmt.Println("  ✅ 同步 .cursortoolset/docs/package-dev-guide.md")
				syncedCount++
			}
		}

		// 同步 workflow
		if syncAll || syncWorkflow {
			if err := syncReleaseWorkflow(cwd); err != nil {
				fmt.Printf("  ⚠️  同步 workflow 失败: %v\n", err)
			} else {
				fmt.Println("  ✅ 同步 .github/workflows/release.yml")
				syncedCount++
			}
		}

		if syncedCount > 0 {
			fmt.Printf("\n✅ 同步完成！已更新 %d 个文件\n", syncedCount)
		} else {
			fmt.Println("\n⚠️  没有文件被更新")
		}

		return nil
	},
}

func init() {
	syncCmd.Flags().BoolVar(&syncAll, "all", false, "同步所有文件（默认行为）")
	syncCmd.Flags().BoolVar(&syncWorkflow, "workflow", false, "仅同步 release workflow")
	syncCmd.Flags().BoolVar(&syncGuide, "guide", false, "仅同步包开发指南")
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

// syncPackageDevGuide 同步包开发指南
func syncPackageDevGuide(targetDir string) error {
	// 确保目录存在
	docsDir := filepath.Join(targetDir, ".cursortoolset", "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		return err
	}

	// 获取安装目录的 docs 路径
	rootDir, err := paths.GetRootDir()
	if err != nil {
		return fmt.Errorf("获取安装目录失败: %w", err)
	}

	srcPath := filepath.Join(rootDir, "docs", "package-dev-guide.md")
	destPath := filepath.Join(docsDir, "package-dev-guide.md")

	// 读取源文件
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("读取包开发指南失败: %w", err)
	}

	// 写入目标文件
	return os.WriteFile(destPath, data, 0644)
}

// syncReleaseWorkflow 同步 release workflow
func syncReleaseWorkflow(targetDir string) error {
	// 确保目录存在
	workflowDir := filepath.Join(targetDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return err
	}

	destPath := filepath.Join(workflowDir, "release.yml")

	// 尝试从安装目录复制 workflow 模板
	rootDir, err := paths.GetRootDir()
	if err == nil {
		srcPath := filepath.Join(rootDir, "docs", "release-workflow-template.yml")
		if data, err := os.ReadFile(srcPath); err == nil {
			return os.WriteFile(destPath, data, 0644)
		}
	}

	// 如果复制失败，使用内置模板（与 init.go 中相同）
	content := `name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    
    steps:
      - uses: actions/checkout@v4
      
      # 获取版本号
      - name: Get version
        id: version
        run: echo "VERSION=${GITHUB_REF#refs/tags/v}" >> $GITHUB_OUTPUT
      
      # 【关键】先同步 package.json 版本号，再打包
      - name: Sync package.json version
        run: |
          VERSION="${{ steps.version.outputs.VERSION }}"
          echo "📌 同步版本号: $VERSION"
          jq --arg version "$VERSION" '.version = $version' package.json > package.json.tmp
          mv package.json.tmp package.json
          echo "✅ package.json 版本已更新"
          cat package.json | jq '{name, version}'
      
      # 打包（此时 package.json 已包含正确版本号）
      - name: Create tarball
        run: |
          PACKAGE_NAME=$(jq -r '.name' package.json)
          mkdir -p /tmp/release
          tar -czvf /tmp/release/${PACKAGE_NAME}-${{ steps.version.outputs.VERSION }}.tar.gz \
            --exclude='.git' \
            --exclude='.github' \
            --exclude='*.tar.gz' \
            .
      
      # 计算 SHA256 并生成最终 package.json
      - name: Generate release package.json
        run: |
          PACKAGE_NAME=$(jq -r '.name' package.json)
          VERSION="${{ steps.version.outputs.VERSION }}"
          TARBALL="${PACKAGE_NAME}-${VERSION}.tar.gz"
          SHA256=$(sha256sum /tmp/release/$TARBALL | cut -d' ' -f1)
          SIZE=$(stat -c%s /tmp/release/$TARBALL)
          
          jq --arg tarball "$TARBALL" \
             --arg sha256 "$SHA256" \
             --arg size "$SIZE" \
             '.dist.tarball = $tarball | .dist.sha256 = $sha256 | .dist.size = ($size | tonumber)' \
             package.json > /tmp/release/package.json
          
          echo "📦 Release package.json:"
          cat /tmp/release/package.json | jq '{name, version, dist}'
      
      # 创建 Release
      - name: Create Release
        uses: softprops/action-gh-release@v1
        with:
          files: |
            /tmp/release/package.json
            /tmp/release/*.tar.gz
          generate_release_notes: true
`
	return os.WriteFile(destPath, []byte(content), 0644)
}
