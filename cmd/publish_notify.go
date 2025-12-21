package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shichao402/Dec/pkg/types"
	"github.com/spf13/cobra"
)

var (
	publishNotifyDryRun bool
	publishNotifyRepo   string
)

var publishNotifyCmd = &cobra.Command{
	Use:   "publish-notify",
	Short: "通知注册表更新包版本",
	Long: `发布包后，通知 Dec 注册表更新包版本。

此命令会：
1. 读取当前目录的 dec_package.json
2. 向 Dec 仓库创建一个 pack-sync Issue
3. Dec 的 CI 会自动处理 Issue 并更新注册表

前置条件：
- 已安装 gh CLI 并登录
- 当前目录有 dec_package.json

示例：
  dec publish-notify              # 通知更新
  dec publish-notify --dry-run    # 预览模式，不实际创建 Issue`,
	RunE: runPublishNotify,
}

func init() {
	RootCmd.AddCommand(publishNotifyCmd)
	publishNotifyCmd.Flags().BoolVar(&publishNotifyDryRun, "dry-run", false, "预览模式，不实际创建 Issue")
	publishNotifyCmd.Flags().StringVar(&publishNotifyRepo, "repo", "shichao402/Dec", "Dec 仓库地址")
}

func runPublishNotify(cmd *cobra.Command, args []string) error {
	// 获取当前目录
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	// 读取包元数据
	pack, err := types.LoadPackFromPath(cwd)
	if err != nil {
		return fmt.Errorf("加载包元数据失败: %w\n\n请确保在包目录中运行此命令", err)
	}

	// 验证必要字段
	if pack.Name == "" {
		return fmt.Errorf("包元数据缺少 name 字段")
	}
	if pack.Version == "" {
		return fmt.Errorf("包元数据缺少 version 字段")
	}
	if pack.Repository.URL == "" {
		return fmt.Errorf("包元数据缺少 repository.url 字段")
	}

	// 构建 Issue 内容
	issueTitle := fmt.Sprintf("[pack-sync] %s@%s", pack.Name, pack.Version)
	issueBody := buildIssueBody(pack)

	fmt.Println("📦 发布通知")
	fmt.Println()
	fmt.Printf("包名: %s\n", pack.Name)
	fmt.Printf("版本: %s\n", pack.Version)
	fmt.Printf("类型: %s\n", pack.Type)
	fmt.Printf("仓库: %s\n", pack.Repository.URL)
	fmt.Println()

	if publishNotifyDryRun {
		fmt.Println("📝 预览模式 - Issue 内容：")
		fmt.Println()
		fmt.Printf("标题: %s\n", issueTitle)
		fmt.Println("---")
		fmt.Println(issueBody)
		fmt.Println("---")
		fmt.Println()
		fmt.Println("💡 移除 --dry-run 参数以实际创建 Issue")
		return nil
	}

	// 检查 gh CLI 是否可用
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("未找到 gh CLI\n\n请安装 GitHub CLI: https://cli.github.com/")
	}

	// 创建 Issue
	fmt.Println("🔄 创建 Issue...")

	ghCmd := exec.Command("gh", "issue", "create",
		"--repo", publishNotifyRepo,
		"--title", issueTitle,
		"--body", issueBody,
		"--label", "pack-sync",
	)
	ghCmd.Stdout = os.Stdout
	ghCmd.Stderr = os.Stderr

	if err := ghCmd.Run(); err != nil {
		return fmt.Errorf("创建 Issue 失败: %w\n\n请确保已登录 gh CLI: gh auth login", err)
	}

	fmt.Println()
	fmt.Println("✅ 发布通知已发送！")
	fmt.Println("   Dec 的 CI 将自动处理并更新注册表")

	return nil
}

// buildIssueBody 构建 Issue 内容
func buildIssueBody(pack *types.Pack) string {
	var sb strings.Builder

	sb.WriteString("## 包同步请求\n\n")
	sb.WriteString("此 Issue 由 `dec publish-notify` 自动创建。\n\n")

	sb.WriteString("### 包信息\n\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString(fmt.Sprintf("  \"name\": \"%s\",\n", pack.Name))
	sb.WriteString(fmt.Sprintf("  \"version\": \"%s\",\n", pack.Version))
	sb.WriteString(fmt.Sprintf("  \"type\": \"%s\",\n", pack.Type))
	sb.WriteString(fmt.Sprintf("  \"repository\": \"%s\"\n", pack.Repository.URL))
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")

	if pack.Description != "" {
		sb.WriteString("### 描述\n\n")
		sb.WriteString(pack.Description)
		sb.WriteString("\n\n")
	}

	sb.WriteString("---\n")
	sb.WriteString("*此 Issue 将由 CI 自动处理*\n")

	return sb.String()
}
