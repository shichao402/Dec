package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/firoyang/CursorToolset/pkg/config"
	"github.com/spf13/cobra"
)

var (
	initDir    string
	initAuthor string
)

var initCmd = &cobra.Command{
	Use:   "init <package-name>",
	Short: "初始化一个新的工具集包项目",
	Long: `初始化一个新的工具集包项目，生成必要的配置文件和目录结构。

生成的文件：
  - toolset.json      包的自描述文件（元数据）
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
		if _, err := os.Stat(targetDir); err == nil {
			// 检查是否已经初始化
			if _, err := os.Stat(filepath.Join(targetDir, "toolset.json")); err == nil {
				return fmt.Errorf("目录 %s 已经是一个工具集包项目", targetDir)
			}
		}

		fmt.Printf("📦 初始化工具集包: %s\n", packageName)
		fmt.Printf("   目录: %s\n\n", targetDir)

		// 创建目录结构
		if err := createPackageStructure(targetDir, packageName); err != nil {
			return fmt.Errorf("创建目录结构失败: %w", err)
		}

		fmt.Println("\n✅ 工具集包初始化完成！")
		fmt.Println("\n📝 下一步：")
		fmt.Printf("   1. 编辑 %s/toolset.json 完善包信息\n", targetDir)
		fmt.Printf("   2. 在 %s 目录下开发你的工具集\n", targetDir)
		fmt.Println("   3. 创建 GitHub Release 发布你的包")
		fmt.Printf("\n📚 参考文档：%s#package-development\n", config.RepoURL)

		return nil
	},
}

func init() {
	initCmd.Flags().StringVarP(&initDir, "dir", "d", "", "目标目录（默认使用包名作为目录名）")
	initCmd.Flags().StringVarP(&initAuthor, "author", "a", "", "作者名称")
	RootCmd.AddCommand(initCmd)
}

// validatePackageName 验证包名
func validatePackageName(name string) error {
	if name == "" {
		return fmt.Errorf("包名不能为空")
	}

	// 包名只能包含小写字母、数字和连字符
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
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
func createPackageStructure(targetDir, packageName string) error {
	// 创建主目录
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	// 创建 toolset.json
	if err := createToolsetJSON(targetDir, packageName); err != nil {
		return fmt.Errorf("创建 toolset.json 失败: %w", err)
	}
	fmt.Println("  ✅ 创建 toolset.json")

	// 创建 README.md
	if err := createReadme(targetDir, packageName); err != nil {
		return fmt.Errorf("创建 README.md 失败: %w", err)
	}
	fmt.Println("  ✅ 创建 README.md")

	// 创建 .cursortoolset 目录和规则文件
	if err := createCursorToolsetDir(targetDir, packageName); err != nil {
		return fmt.Errorf("创建 .cursortoolset 目录失败: %w", err)
	}
	fmt.Println("  ✅ 创建 .cursortoolset/ 规则目录")

	// 创建 .gitignore
	if err := createGitignore(targetDir); err != nil {
		return fmt.Errorf("创建 .gitignore 失败: %w", err)
	}
	fmt.Println("  ✅ 创建 .gitignore")

	return nil
}

// createToolsetJSON 创建 toolset.json
func createToolsetJSON(targetDir, packageName string) error {
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
			"tarball": fmt.Sprintf("https://github.com/YOUR_USERNAME/%s/releases/download/v0.1.0/%s-0.1.0.tar.gz", packageName, packageName),
			"sha256":  "TODO: 发布时填写 SHA256",
		},
		"cursortoolset": map[string]string{
			"minVersion": "1.0.0",
		},
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(targetDir, "toolset.json"), data, 0644)
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
├── toolset.json          # 包配置文件
├── .cursortoolset/       # AI 规则目录
│   └── rules/            # 规则文件
├── rules/                # 你的规则文件
└── README.md
`+"```"+`

### 发布

1. 更新 `+"`toolset.json`"+` 中的版本号
2. 创建 Git Tag: `+"`git tag v0.1.0`"+`
3. 在 GitHub 创建 Release 并上传打包文件

## 许可证

MIT
`, toDisplayName(packageName), toDisplayName(packageName), packageName, packageName)

	return os.WriteFile(filepath.Join(targetDir, "README.md"), []byte(content), 0644)
}

// createCursorToolsetDir 创建 .cursortoolset 目录
func createCursorToolsetDir(targetDir, packageName string) error {
	cursorDir := filepath.Join(targetDir, ".cursortoolset")
	rulesDir := filepath.Join(cursorDir, "rules")

	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return err
	}

	// 创建开发指南规则
	devGuide := fmt.Sprintf(`# %s 开发指南

## 包结构规范

本包遵循 CursorToolset 包规范：

1. **toolset.json** - 包的元数据文件，包含：
   - name: 包名（必须与目录名一致）
   - version: 语义化版本号 (SemVer)
   - dist.tarball: 下载地址
   - dist.sha256: 校验和

2. **版本号规范** - 使用语义化版本：
   - MAJOR.MINOR.PATCH
   - 例如: 1.0.0, 1.2.3

3. **发布流程**：
   - 更新 toolset.json 中的 version
   - 创建 Git Tag (v1.0.0)
   - 打包: tar -czvf %s-VERSION.tar.gz *
   - 计算 SHA256 并更新 toolset.json
   - 在 GitHub Release 发布

## AI 规则编写指南

在 rules/ 目录下创建 .md 文件作为 AI 规则。
`, toDisplayName(packageName), packageName)

	return os.WriteFile(filepath.Join(rulesDir, "dev-guide.md"), []byte(devGuide), 0644)
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
