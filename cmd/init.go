package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/firoyang/CursorToolset/pkg/config"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/spf13/cobra"
)

var (
	initDir    string
	initAuthor string
	initForce  bool
)

var initCmd = &cobra.Command{
	Use:   "init <package-name>",
	Short: "初始化一个新的工具集包项目",
	Long: `初始化一个新的工具集包项目，生成必要的配置文件和目录结构。

生成的文件：
  - package.json      包的元数据文件
  - README.md         包说明文档
  - .cursortoolset/   包开发规则和指南

示例：
  # 在当前目录初始化
  cursortoolset init my-toolset

  # 在指定目录初始化
  cursortoolset init my-toolset --dir ./packages/my-toolset`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		packageName := args[0]

		// 验证包名
		if err := validatePackageName(packageName); err != nil {
			return err
		}

		// 确定目标目录
		targetDir := initDir
		if targetDir == "" {
			targetDir = packageName
		}

		// 检查目录是否已存在
		existingProject := false
		if _, err := os.Stat(targetDir); err == nil {
			// 检查是否已经初始化（支持新旧两种文件名）
			packageJsonExists := false
			if _, err := os.Stat(filepath.Join(targetDir, "package.json")); err == nil {
				packageJsonExists = true
			}
			toolsetJsonExists := false
			if _, err := os.Stat(filepath.Join(targetDir, "toolset.json")); err == nil {
				toolsetJsonExists = true
			}
			if packageJsonExists || toolsetJsonExists {
				if !initForce {
					return fmt.Errorf("目录 %s 已经是一个工具集包项目\n\n提示: 使用 --force 强制重新初始化", targetDir)
				}
				existingProject = true
			}
		}

		if existingProject {
			fmt.Printf("🔄 重新初始化工具集包: %s\n", packageName)
		} else {
			fmt.Printf("📦 初始化工具集包: %s\n", packageName)
		}
		fmt.Printf("   目录: %s\n\n", targetDir)

		// 创建/更新目录结构
		if err := createPackageStructure(targetDir, packageName, existingProject); err != nil {
			return fmt.Errorf("创建目录结构失败: %w", err)
		}

		if existingProject {
			fmt.Println("\n✅ 工具集包重新初始化完成！")
		} else {
			fmt.Println("\n✅ 工具集包初始化完成！")
		}
		fmt.Println("\n📝 下一步：")
		fmt.Printf("   1. 编辑 %s/package.json 完善包信息\n", targetDir)
		fmt.Printf("   2. 在 %s 目录下开发你的工具集\n", targetDir)
		fmt.Println("   3. 创建 GitHub Release 发布你的包")
		fmt.Printf("\n📚 参考文档：%s#package-development\n", config.GetRepoURL())

		return nil
	},
}

func init() {
	initCmd.Flags().StringVarP(&initDir, "dir", "d", "", "目标目录（默认使用包名作为目录名）")
	initCmd.Flags().StringVarP(&initAuthor, "author", "a", "", "作者名称")
	initCmd.Flags().BoolVarP(&initForce, "force", "f", false, "强制重新初始化已有项目")
	RootCmd.AddCommand(initCmd)
}

// validatePackageName 验证包名
func validatePackageName(name string) error {
	if name == "" {
		return fmt.Errorf("包名不能为空")
	}

	// 包名只能包含小写字母、数字和连字符
	for _, c := range name {
		isLowerLetter := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		isHyphen := c == '-'
		if !isLowerLetter && !isDigit && !isHyphen {
			return fmt.Errorf("包名只能包含小写字母、数字和连字符: %s", name)
		}
	}

	// 不能以连字符开头或结尾
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("包名不能以连字符开头或结尾: %s", name)
	}

	return nil
}

// createPackageStructure 创建包目录结构
func createPackageStructure(targetDir, packageName string, isReinit bool) error {
	// 创建主目录
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	// 创建/更新 package.json
	if err := createPackageJSON(targetDir, packageName, isReinit); err != nil {
		return fmt.Errorf("创建 package.json 失败: %w", err)
	}
	if isReinit {
		fmt.Println("  ✅ 更新 package.json")
	} else {
		fmt.Println("  ✅ 创建 package.json")
	}

	// 创建 README.md（仅新项目或不存在时）
	readmePath := filepath.Join(targetDir, "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		if err := createReadme(targetDir, packageName); err != nil {
			return fmt.Errorf("创建 README.md 失败: %w", err)
		}
		fmt.Println("  ✅ 创建 README.md")
	} else if isReinit {
		fmt.Println("  ⏭️  跳过 README.md（已存在）")
	}

	// 创建 .cursortoolset 目录和规则文件
	cursorDir := filepath.Join(targetDir, ".cursortoolset")
	if _, err := os.Stat(cursorDir); os.IsNotExist(err) {
		if err := createCursorToolsetDir(targetDir, packageName); err != nil {
			return fmt.Errorf("创建 .cursortoolset 目录失败: %w", err)
		}
		fmt.Println("  ✅ 创建 .cursortoolset/ 规则目录")
	} else if isReinit {
		// --force 模式：检查并补充缺失的文件
		fmt.Println("  📂 检查 .cursortoolset/")
		if err := ensureCursorToolsetFiles(targetDir, packageName); err != nil {
			fmt.Printf("    ⚠️  补充文件失败: %v\n", err)
		}
	}

	// 创建 .github/workflows/release.yml（仅新项目或不存在时）
	workflowPath := filepath.Join(targetDir, ".github", "workflows", "release.yml")
	if _, err := os.Stat(workflowPath); os.IsNotExist(err) {
		if err := createReleaseWorkflow(targetDir); err != nil {
			fmt.Printf("  ⚠️  创建 release workflow 失败: %v\n", err)
		} else {
			fmt.Println("  ✅ 创建 .github/workflows/release.yml")
		}
	} else if isReinit {
		fmt.Println("  ⏭️  跳过 .github/workflows/release.yml（已存在）")
	}

	// 创建 .gitignore（仅新项目或不存在时）
	gitignorePath := filepath.Join(targetDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		if err := createGitignore(targetDir); err != nil {
			return fmt.Errorf("创建 .gitignore 失败: %w", err)
		}
		fmt.Println("  ✅ 创建 .gitignore")
	} else if isReinit {
		fmt.Println("  ⏭️  跳过 .gitignore（已存在）")
	}

	return nil
}

// createPackageJSON 创建或更新 package.json
func createPackageJSON(targetDir, packageName string, isReinit bool) error {
	manifestPath := filepath.Join(targetDir, "package.json")

	// 如果是重新初始化，尝试读取现有配置（支持新旧两种文件名）
	var existingData map[string]interface{}
	if isReinit {
		// 优先读取 package.json
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			// 回退到 toolset.json
			data, err = os.ReadFile(filepath.Join(targetDir, "toolset.json"))
		}
		if err == nil {
			_ = json.Unmarshal(data, &existingData)
		}
	}

	// 构建新的 manifest
	manifest := map[string]interface{}{
		"name":        packageName,
		"displayName": toDisplayName(packageName),
		"version":     "0.1.0",
		"description": "TODO: 添加包描述",
		"author":      initAuthor,
		"license":     "MIT",
		"keywords":    []string{},
		"repository": map[string]string{
			"type": "git",
			"url":  fmt.Sprintf("https://github.com/YOUR_USERNAME/%s.git", packageName),
		},
		"dist": map[string]string{
			"tarball": fmt.Sprintf("%s-0.1.0.tar.gz", packageName),
			"sha256":  "TODO: 发布时自动填写",
		},
		"cursortoolset": map[string]string{
			"minVersion": "1.0.0",
		},
	}

	// 如果是重新初始化，保留用户自定义的值
	if isReinit && existingData != nil {
		// 保留用户设置的字段
		preserveFields := []string{"version", "description", "author", "license", "keywords", "repository", "dist", "bin", "build", "release", "dependencies"}
		for _, field := range preserveFields {
			if val, ok := existingData[field]; ok {
				manifest[field] = val
			}
		}
		// 确保 name 和 displayName 使用新值（如果包名改变）
		manifest["name"] = packageName
		if existingData["displayName"] == nil || existingData["displayName"] == "" {
			manifest["displayName"] = toDisplayName(packageName)
		} else {
			manifest["displayName"] = existingData["displayName"]
		}
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	// 如果存在旧的 toolset.json，删除它
	oldPath := filepath.Join(targetDir, "toolset.json")
	if _, err := os.Stat(oldPath); err == nil {
		_ = os.Remove(oldPath)
		fmt.Println("  🔄 迁移 toolset.json -> package.json")
	}

	return os.WriteFile(manifestPath, data, 0644)
}

// createReadme 创建 README.md
func createReadme(targetDir, packageName string) error {
	content := fmt.Sprintf(`# %s

%s 的 AI 工具集。

## 安装

`+"```bash"+`
cursortoolset install %s
`+"```"+`

## 功能

TODO: 描述你的工具集功能

## 使用方法

TODO: 添加使用说明

## 开发

### 目录结构

`+"```"+`
%s/
├── package.json          # 包配置文件
├── .cursortoolset/       # AI 规则目录
│   └── docs/             # 开发文档
├── rules/                # 你的规则文件
└── README.md
`+"```"+`

### 发布

1. 更新 `+"`package.json`"+` 中的版本号
2. 创建 Git Tag: `+"`git tag v0.1.0`"+`
3. 推送 Tag 触发自动发布: `+"`git push origin v0.1.0`"+`

## 许可证

MIT
`, toDisplayName(packageName), toDisplayName(packageName), packageName, packageName)

	return os.WriteFile(filepath.Join(targetDir, "README.md"), []byte(content), 0644)
}

// createCursorToolsetDir 创建 .cursortoolset 目录
func createCursorToolsetDir(targetDir, packageName string) error {
	cursorDir := filepath.Join(targetDir, ".cursortoolset")
	docsDir := filepath.Join(cursorDir, "docs")

	if err := os.MkdirAll(docsDir, 0755); err != nil {
		return err
	}

	// 尝试从安装目录复制包开发指南
	if err := copyPackageDevGuide(docsDir); err != nil {
		// 如果复制失败，生成一个简单的开发指南
		fmt.Printf("  ⚠️  无法复制包开发指南: %v\n", err)
		if err := createFallbackDevGuide(docsDir, packageName); err != nil {
			return err
		}
	}

	return nil
}

// ensureCursorToolsetFiles 检查并补充 .cursortoolset 目录中缺失的文件
func ensureCursorToolsetFiles(targetDir, packageName string) error {
	cursorDir := filepath.Join(targetDir, ".cursortoolset")
	docsDir := filepath.Join(cursorDir, "docs")

	// 检查 docs 目录
	if _, err := os.Stat(docsDir); os.IsNotExist(err) {
		if err := os.MkdirAll(docsDir, 0755); err != nil {
			return err
		}
		fmt.Println("    ✅ 补充 docs/ 目录")
	}

	// 检查 package-dev-guide.md
	devGuidePath := filepath.Join(docsDir, "package-dev-guide.md")
	if _, err := os.Stat(devGuidePath); os.IsNotExist(err) {
		if err := copyPackageDevGuide(docsDir); err != nil {
			// 复制失败，使用备用指南
			if err := createFallbackDevGuide(docsDir, packageName); err != nil {
				return err
			}
			fmt.Println("    ✅ 补充 docs/package-dev-guide.md（备用版）")
		} else {
			fmt.Println("    ✅ 补充 docs/package-dev-guide.md")
		}
	} else {
		fmt.Println("    ⏭️  跳过 docs/package-dev-guide.md（已存在）")
	}

	return nil
}

// copyPackageDevGuide 从安装目录复制包开发指南
func copyPackageDevGuide(destDir string) error {
	// 获取安装目录的 docs 路径
	rootDir, err := paths.GetRootDir()
	if err != nil {
		return err
	}

	srcPath := filepath.Join(rootDir, "docs", "package-dev-guide.md")
	destPath := filepath.Join(destDir, "package-dev-guide.md")

	// 读取源文件
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("读取包开发指南失败: %w", err)
	}

	// 写入目标文件
	return os.WriteFile(destPath, data, 0644)
}

// createFallbackDevGuide 创建备用开发指南
func createFallbackDevGuide(destDir, packageName string) error {
	devGuide := fmt.Sprintf(`# %s 开发指南

## 包结构规范

本包遵循 CursorToolset 包规范：

1. **package.json** - 包的元数据文件，包含：
   - name: 包名（必须与目录名一致）
   - version: 语义化版本号 (SemVer)
   - dist.tarball: 下载文件名（相对路径）
   - dist.sha256: 校验和（发布时自动生成）

2. **版本号规范** - 使用语义化版本：
   - MAJOR.MINOR.PATCH
   - 例如: 1.0.0, 1.2.3

3. **发布流程**：
   - 更新 package.json 中的 version
   - 创建 Git Tag (v1.0.0)
   - 推送 Tag 触发自动发布

## 更多信息

运行 cursortoolset update 更新管理器以获取完整的包开发指南。
`, toDisplayName(packageName))

	return os.WriteFile(filepath.Join(destDir, "package-dev-guide.md"), []byte(devGuide), 0644)
}

// createGitignore 创建 .gitignore
func createGitignore(targetDir string) error {
	content := `# OS
.DS_Store
Thumbs.db

# IDE
.idea/
.vscode/
*.swp
*.swo

# Build
dist/
*.tar.gz

# Logs
*.log
`
	return os.WriteFile(filepath.Join(targetDir, ".gitignore"), []byte(content), 0644)
}

// createReleaseWorkflow 创建 GitHub Actions release workflow
func createReleaseWorkflow(targetDir string) error {
	workflowDir := filepath.Join(targetDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return err
	}

	// 尝试从安装目录复制 workflow 模板
	rootDir, err := paths.GetRootDir()
	if err == nil {
		srcPath := filepath.Join(rootDir, "docs", "release-workflow-template.yml")
		if data, err := os.ReadFile(srcPath); err == nil {
			return os.WriteFile(filepath.Join(workflowDir, "release.yml"), data, 0644)
		}
	}

	// 如果复制失败，使用内置模板
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
      
      # 打包（输出到 /tmp/release 避免打包时文件变化）
      - name: Create tarball
        run: |
          PACKAGE_NAME=$(jq -r '.name' package.json)
          mkdir -p /tmp/release
          tar -czvf /tmp/release/${PACKAGE_NAME}-${{ steps.version.outputs.VERSION }}.tar.gz \
            --exclude='.git' \
            --exclude='.github' \
            --exclude='*.tar.gz' \
            .
      
      # 计算 SHA256 并更新 package.json
      - name: Generate release package.json
        run: |
          PACKAGE_NAME=$(jq -r '.name' package.json)
          TARBALL="${PACKAGE_NAME}-${{ steps.version.outputs.VERSION }}.tar.gz"
          SHA256=$(sha256sum /tmp/release/$TARBALL | cut -d' ' -f1)
          SIZE=$(stat -c%s /tmp/release/$TARBALL)
          
          jq --arg tarball "$TARBALL" \
             --arg sha256 "$SHA256" \
             --arg size "$SIZE" \
             --arg version "${{ steps.version.outputs.VERSION }}" \
             '.version = $version | .dist.tarball = $tarball | .dist.sha256 = $sha256 | .dist.size = ($size | tonumber)' \
             package.json > /tmp/release/package.json
      
      # 创建 Release
      - name: Create Release
        uses: softprops/action-gh-release@v1
        with:
          files: |
            /tmp/release/package.json
            /tmp/release/*.tar.gz
          generate_release_notes: true
`
	return os.WriteFile(filepath.Join(workflowDir, "release.yml"), []byte(content), 0644)
}

// toDisplayName 将包名转换为显示名称
func toDisplayName(name string) string {
	parts := strings.Split(name, "-")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}
