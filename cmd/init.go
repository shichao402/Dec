package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/spf13/cobra"
)

var (
	initProjectName string
	initProjectIDEs []string
)

var initNewCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化项目 Dec 配置",
	Long: `初始化项目的 Dec 配置，创建 .dec/config/ 目录结构。

生成的配置文件：
  .dec/config/
  ├── project.json      项目信息
  ├── technology.json   技术栈配置
  └── packs.json        启用的包配置

示例：
  dec init                        # 交互式初始化
  dec init --name my-project      # 指定项目名
  dec init --ide cursor           # 指定目标 IDE`,
	RunE: runInitProject,
}

func init() {
	RootCmd.AddCommand(initNewCmd)
	initNewCmd.Flags().StringVar(&initProjectName, "name", "", "项目名称")
	initNewCmd.Flags().StringSliceVar(&initProjectIDEs, "ide", []string{"cursor"}, "目标 IDE (cursor, codebuddy, windsurf, trae)")
}

func runInitProject(cmd *cobra.Command, args []string) error {
	// 获取当前目录
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	// 检查是否已初始化
	mgr := config.NewProjectConfigManager(cwd)
	if mgr.Exists() {
		fmt.Println("⚠️  项目已初始化")
		fmt.Println()
		fmt.Println("💡 运行 dec sync 同步规则和 MCP 配置")
		return nil
	}

	// 确定项目名称
	projectName := initProjectName
	if projectName == "" {
		projectName = filepath.Base(cwd)
	}

	fmt.Printf("📦 初始化 Dec 配置: %s\n", projectName)
	fmt.Printf("   目录: %s\n\n", cwd)

	// 初始化项目
	if err := mgr.InitProject(projectName, initProjectIDEs); err != nil {
		return fmt.Errorf("初始化失败: %w", err)
	}

	fmt.Println("  ✅ 创建 .dec/config/project.json")
	fmt.Println("  ✅ 创建 .dec/config/technology.json")
	fmt.Println("  ✅ 创建 .dec/config/packs.json")

	fmt.Println("\n✅ 初始化完成！")
	fmt.Println("\n📝 下一步：")
	fmt.Println("   1. 编辑 .dec/config/technology.json 配置技术栈")
	fmt.Println("   2. 编辑 .dec/config/packs.json 启用需要的包")
	fmt.Println("   3. 运行 dec sync 同步规则和 MCP 配置")

	return nil
}

// DetectTechnology 检测项目技术栈
func DetectTechnology(projectRoot string) *types.TechnologyConfig {
	tech := &types.TechnologyConfig{}

	// 检测语言
	if fileExists(filepath.Join(projectRoot, "go.mod")) {
		tech.Languages = append(tech.Languages, "go")
	}
	if fileExists(filepath.Join(projectRoot, "pubspec.yaml")) {
		tech.Languages = append(tech.Languages, "dart")
		tech.Frameworks = append(tech.Frameworks, "flutter")
	}
	if fileExists(filepath.Join(projectRoot, "package.json")) {
		// 可能是 Node.js 项目
		tech.Languages = append(tech.Languages, "typescript")
	}
	if fileExists(filepath.Join(projectRoot, "requirements.txt")) || fileExists(filepath.Join(projectRoot, "pyproject.toml")) {
		tech.Languages = append(tech.Languages, "python")
	}

	return tech
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
