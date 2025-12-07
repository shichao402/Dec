package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	releaseMajor   bool
	releaseMinor   bool
	releasePatch   bool
	releaseDryRun  bool
	releaseSkipTag bool
	releaseWait    bool
)

var releaseCmd = &cobra.Command{
	Use:   "release",
	Short: "发布新版本",
	Long: `发布新版本，自动完成以下步骤：

  1. 提升版本号（默认 patch）
  2. 打包并计算 SHA256
  3. 更新 package.json
  4. 创建 Git commit 和 tag
  5. 推送到远程仓库

选项：
  --wait       推送后等待 GitHub Actions 完成并确认 Release 创建成功

示例：
  cursortoolset release              # 发布 patch 版本
  cursortoolset release --minor      # 发布 minor 版本
  cursortoolset release --major      # 发布 major 版本
  cursortoolset release --wait       # 发布并等待 CI 完成
  cursortoolset release --dry-run    # 预览发布流程，不执行`,
	RunE: runRelease,
}

func init() {
	releaseCmd.Flags().BoolVar(&releaseMajor, "major", false, "发布主版本 (x.0.0)")
	releaseCmd.Flags().BoolVar(&releaseMinor, "minor", false, "发布次版本 (0.x.0)")
	releaseCmd.Flags().BoolVar(&releasePatch, "patch", false, "发布补丁版本 (0.0.x)")
	releaseCmd.Flags().BoolVar(&releaseDryRun, "dry-run", false, "预览模式，不执行实际操作")
	releaseCmd.Flags().BoolVar(&releaseSkipTag, "skip-tag", false, "跳过 Git tag 和 push")
	releaseCmd.Flags().BoolVar(&releaseWait, "wait", false, "等待 GitHub Actions 完成并确认 Release 创建")
	RootCmd.AddCommand(releaseCmd)
}

func runRelease(cmd *cobra.Command, args []string) error {
	// 检查是否在 git 仓库中
	if !isGitRepo() {
		return fmt.Errorf("当前目录不是 Git 仓库")
	}

	// 检查工作区是否干净
	if !releaseDryRun && !isGitClean() {
		return fmt.Errorf("git 工作区有未提交的更改，请先提交或暂存")
	}

	// 加载 manifest
	manifest, manifestPath, err := loadManifest()
	if err != nil {
		return err
	}

	packageName := manifest["name"].(string)
	currentVersion := manifest["version"].(string)

	// 确定版本提升类型
	bumpType := "patch"
	count := 0
	if releaseMajor {
		bumpType = "major"
		count++
	}
	if releaseMinor {
		bumpType = "minor"
		count++
	}
	if releasePatch {
		bumpType = "patch"
		count++
	}
	if count > 1 {
		return fmt.Errorf("只能指定一种版本提升类型")
	}

	// 计算新版本号
	newVersion, err := calculateNewVersion(currentVersion, bumpType)
	if err != nil {
		return err
	}

	fmt.Printf("🚀 发布 %s\n", packageName)
	fmt.Printf("   版本: %s -> %s\n", currentVersion, newVersion)
	if releaseDryRun {
		fmt.Printf("   模式: 预览模式 (dry-run)\n")
	}
	fmt.Println()

	// dry-run 模式：显示详细预览
	if releaseDryRun {
		return runReleaseDryRun(manifest, packageName, newVersion)
	}

	// Step 1: 更新版本号
	fmt.Println("📝 Step 1: 更新版本号")
	if err := updateVersionInManifest(manifest, manifestPath, currentVersion, newVersion); err != nil {
		return fmt.Errorf("更新版本号失败: %w", err)
	}
	fmt.Printf("   ✅ package.json 版本已更新为 %s\n\n", newVersion)

	// Step 2: 打包
	fmt.Println("📦 Step 2: 打包")
	outputFile := fmt.Sprintf("%s-%s.tar.gz", packageName, newVersion)
	// 直接调用 pack 逻辑
	packOutput = outputFile
	packVerify = true
	if err := runPack(nil, []string{"."}); err != nil {
		return fmt.Errorf("打包失败: %w", err)
	}

	// Step 3: Git commit (SHA256 已在 pack --verify 中更新)
	fmt.Println("📝 Step 3: Git commit")
	commitMsg := fmt.Sprintf("chore: release v%s", newVersion)
	if err := gitAdd("package.json"); err != nil {
		return fmt.Errorf("git add 失败: %w", err)
	}
	if err := gitCommit(commitMsg); err != nil {
		return fmt.Errorf("git commit 失败: %w", err)
	}
	fmt.Printf("   ✅ 已提交: %s\n\n", commitMsg)

	// Step 4: Git tag
	if !releaseSkipTag {
		fmt.Println("🏷️  Step 4: Git tag")
		tagName := fmt.Sprintf("v%s", newVersion)
		if err := gitTag(tagName); err != nil {
			return fmt.Errorf("git tag 失败: %w", err)
		}
		fmt.Printf("   ✅ 已创建标签: %s\n\n", tagName)

		// Step 5: Git push
		fmt.Println("🚀 Step 5: Git push")
		if err := gitPush(); err != nil {
			fmt.Printf("   ⚠️  推送失败: %v\n", err)
			fmt.Println("   💡 请手动执行: git push && git push --tags")
		} else {
			if err := gitPushTags(); err != nil {
				fmt.Printf("   ⚠️  推送标签失败: %v\n", err)
				fmt.Println("   💡 请手动执行: git push --tags")
			} else {
				fmt.Println("   ✅ 已推送到远程仓库")
			}
		}
		fmt.Println()
	}

	// 完成
	fmt.Println("✅ 发布完成！")
	fmt.Println()

	// --wait 模式：等待 GitHub Actions 完成
	if releaseWait && !releaseSkipTag {
		tagName := fmt.Sprintf("v%s", newVersion)
		if err := waitForRelease(tagName); err != nil {
			return fmt.Errorf("等待发布完成失败: %w", err)
		}
	} else {
		fmt.Println("💡 下一步：")
		fmt.Printf("   1. 在 GitHub 创建 Release (v%s)\n", newVersion)
		fmt.Printf("   2. 上传 %s 到 Release\n", outputFile)
		if releaseSkipTag {
			fmt.Printf("   3. 创建并推送 Git tag:\n")
			fmt.Printf("      git tag v%s && git push --tags\n", newVersion)
		}
	}

	return nil
}

// runReleaseDryRun 执行 dry-run 预览
func runReleaseDryRun(manifest map[string]interface{}, packageName, newVersion string) error {
	fmt.Println("📋 发布预览")
	fmt.Println("=" + strings.Repeat("=", 50))
	fmt.Println()

	// 显示将要包含的文件
	fmt.Println("📦 将要打包的文件:")
	includedFiles, excludedFiles := previewPackageFiles()
	for _, f := range includedFiles {
		fmt.Printf("   ✅ %s\n", f)
	}
	fmt.Println()

	// 显示将要排除的文件
	if len(excludedFiles) > 0 {
		fmt.Println("🚫 将要排除的文件/目录:")
		for _, f := range excludedFiles {
			fmt.Printf("   ❌ %s\n", f)
		}
		fmt.Println()
	}

	// 检查 bin 配置
	if bin, ok := manifest["bin"].(map[string]interface{}); ok && len(bin) > 0 {
		fmt.Println("🔧 可执行文件检查:")
		allBinOk := true
		for cmdName, binConfig := range bin {
			switch v := binConfig.(type) {
			case map[string]interface{}:
				// 多平台格式
				for platform, pathVal := range v {
					if pathStr, ok := pathVal.(string); ok {
						if _, err := os.Stat(pathStr); os.IsNotExist(err) {
							fmt.Printf("   ❌ %s (%s): 文件不存在 - %s\n", cmdName, platform, pathStr)
							allBinOk = false
						} else {
							fmt.Printf("   ✅ %s (%s): %s\n", cmdName, platform, pathStr)
						}
					}
				}
			case string:
				// 简单格式
				if _, err := os.Stat(v); os.IsNotExist(err) {
					fmt.Printf("   ❌ %s: 文件不存在 - %s\n", cmdName, v)
					allBinOk = false
				} else {
					fmt.Printf("   ✅ %s: %s\n", cmdName, v)
				}
			}
		}
		if !allBinOk {
			fmt.Println()
			fmt.Println("⚠️  警告: 部分 bin 文件不存在，请先构建")
		}
		fmt.Println()
	}

	// 显示将要生成的产物
	fmt.Println("📤 发布产物:")
	tarballName := fmt.Sprintf("%s-%s.tar.gz", packageName, newVersion)
	fmt.Printf("   📦 %s\n", tarballName)
	fmt.Printf("   📄 package.json\n")
	fmt.Println()

	// 显示将要执行的 Git 操作
	fmt.Println("🔀 Git 操作:")
	fmt.Printf("   📝 commit: chore: release v%s\n", newVersion)
	fmt.Printf("   🏷️  tag: v%s\n", newVersion)
	fmt.Printf("   🚀 push: origin\n")
	fmt.Println()

	fmt.Println("=" + strings.Repeat("=", 50))
	fmt.Println("💡 这是预览模式，没有执行任何实际操作")
	fmt.Println("   移除 --dry-run 参数以执行实际发布")

	return nil
}

// previewPackageFiles 预览将要打包的文件
func previewPackageFiles() (included []string, excluded []string) {
	// 默认排除规则
	defaultExcludes := []string{
		".git",
		".github",
		"*.tar.gz",
		"*.go",
		"go.mod",
		"go.sum",
		"node_modules",
		".DS_Store",
	}

	// 遍历当前目录
	entries, err := os.ReadDir(".")
	if err != nil {
		return nil, nil
	}

	for _, entry := range entries {
		name := entry.Name()
		isExcluded := false

		for _, pattern := range defaultExcludes {
			if strings.HasPrefix(pattern, "*.") {
				// 扩展名匹配
				if strings.HasSuffix(name, pattern[1:]) {
					isExcluded = true
					break
				}
			} else if name == pattern {
				isExcluded = true
				break
			}
		}

		if isExcluded {
			excluded = append(excluded, name)
		} else {
			if entry.IsDir() {
				included = append(included, name+"/")
			} else {
				included = append(included, name)
			}
		}
	}

	return included, excluded
}

// calculateNewVersion 计算新版本号
func calculateNewVersion(current, bumpType string) (string, error) {
	parts := strings.Split(current, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("当前版本号格式不正确: %s", current)
	}

	var major, minor, patch int
	_, _ = fmt.Sscanf(parts[0], "%d", &major)
	_, _ = fmt.Sscanf(parts[1], "%d", &minor)
	_, _ = fmt.Sscanf(parts[2], "%d", &patch)

	switch bumpType {
	case "major":
		major++
		minor = 0
		patch = 0
	case "minor":
		minor++
		patch = 0
	case "patch":
		patch++
	}

	return fmt.Sprintf("%d.%d.%d", major, minor, patch), nil
}

// updateVersionInManifest 更新 manifest 中的版本号
func updateVersionInManifest(manifest map[string]interface{}, path, oldVersion, newVersion string) error {
	manifest["version"] = newVersion

	// 更新 dist.tarball 中的版本号
	if dist, ok := manifest["dist"].(map[string]interface{}); ok {
		if tarball, ok := dist["tarball"].(string); ok {
			newTarball := strings.ReplaceAll(tarball, oldVersion, newVersion)
			newTarball = strings.ReplaceAll(newTarball, "v"+oldVersion, "v"+newVersion)
			dist["tarball"] = newTarball
		}
	}

	return saveManifest(manifest, path)
}

// saveManifest 保存 manifest
func saveManifest(manifest map[string]interface{}, path string) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Git 操作函数
func isGitRepo() bool {
	_, err := os.Stat(".git")
	return err == nil
}

func isGitClean() bool {
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) == 0
}

func gitAdd(files ...string) error {
	args := append([]string{"add"}, files...)
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func gitCommit(message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func gitTag(tag string) error {
	cmd := exec.Command("git", "tag", tag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func gitPush() error {
	cmd := exec.Command("git", "push")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func gitPushTags() error {
	cmd := exec.Command("git", "push", "--tags")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// waitForRelease 等待 GitHub Actions 完成并确认 Release 创建
func waitForRelease(tagName string) error {
	// 检查 gh CLI 是否可用
	if !isGhAvailable() {
		return fmt.Errorf("gh CLI 未安装或未认证，请先安装并运行 'gh auth login'")
	}

	// 获取仓库信息
	repo, err := getGitRemoteRepo()
	if err != nil {
		return fmt.Errorf("获取仓库信息失败: %w", err)
	}

	fmt.Printf("\n⏳ 等待 GitHub Actions 完成...\n")
	fmt.Printf("   仓库: %s\n", repo)
	fmt.Printf("   标签: %s\n", tagName)
	fmt.Println()

	// 轮询配置
	const (
		pollInterval = 10 * time.Second
		maxWaitTime  = 30 * time.Minute
	)

	startTime := time.Now()
	lastStatus := ""

	for {
		elapsed := time.Since(startTime)
		if elapsed > maxWaitTime {
			return fmt.Errorf("等待超时（%v），请手动检查 GitHub Actions 状态", maxWaitTime)
		}

		// 检查 workflow run 状态
		status, conclusion, err := getWorkflowRunStatus(repo, tagName)
		if err != nil {
			// 可能 workflow 还没开始，继续等待
			if elapsed < 30*time.Second {
				fmt.Printf("   ⏳ 等待 workflow 启动... (%v)\n", elapsed.Round(time.Second))
				time.Sleep(pollInterval)
				continue
			}
			return fmt.Errorf("获取 workflow 状态失败: %w", err)
		}

		// 状态变化时输出
		currentStatus := fmt.Sprintf("%s/%s", status, conclusion)
		if currentStatus != lastStatus {
			switch status {
			case "queued":
				fmt.Printf("   🔄 Workflow 排队中... (%v)\n", elapsed.Round(time.Second))
			case "in_progress":
				fmt.Printf("   🔄 Workflow 运行中... (%v)\n", elapsed.Round(time.Second))
			case "completed":
				switch conclusion {
				case "success":
					fmt.Printf("   ✅ Workflow 完成！\n")
				case "failure":
					return fmt.Errorf("workflow 执行失败，请检查 GitHub Actions 日志")
				case "cancelled":
					return fmt.Errorf("workflow 被取消")
				default:
					return fmt.Errorf("workflow 结束，状态: %s", conclusion)
				}
			}
			lastStatus = currentStatus
		}

		// workflow 完成后检查 Release
		if status == "completed" && conclusion == "success" {
			fmt.Printf("\n⏳ 检查 Release 状态...\n")

			// 等待 Release 创建（可能有延迟）
			for i := 0; i < 6; i++ {
				exists, releaseURL, err := checkReleaseExists(repo, tagName)
				if err != nil {
					fmt.Printf("   ⚠️  检查 Release 失败: %v\n", err)
				}
				if exists {
					fmt.Printf("   ✅ Release 已创建: %s\n", releaseURL)
					fmt.Println()
					fmt.Println("🎉 发布完成！所有步骤已成功执行。")
					return nil
				}
				if i < 5 {
					fmt.Printf("   ⏳ Release 尚未创建，等待中... (%d/6)\n", i+1)
					time.Sleep(5 * time.Second)
				}
			}

			// Release 未创建，但 workflow 成功
			fmt.Printf("   ⚠️  Workflow 成功但 Release 未找到\n")
			fmt.Printf("   💡 请手动检查: https://github.com/%s/releases\n", repo)
			return nil
		}

		time.Sleep(pollInterval)
	}
}

// isGhAvailable 检查 gh CLI 是否可用
func isGhAvailable() bool {
	cmd := exec.Command("gh", "auth", "status")
	return cmd.Run() == nil
}

// getGitRemoteRepo 获取 git remote 仓库信息 (owner/repo 格式)
func getGitRemoteRepo() (string, error) {
	cmd := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// getWorkflowRunStatus 获取指定 tag 触发的 workflow run 状态
func getWorkflowRunStatus(repo, tagName string) (status, conclusion string, err error) {
	// 使用 gh api 查询最近的 workflow runs
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("/repos/%s/actions/runs", repo),
		"-q", fmt.Sprintf(".workflow_runs[] | select(.head_branch == \"%s\" or .head_branch == \"refs/tags/%s\") | {status: .status, conclusion: .conclusion}", tagName, tagName),
		"--paginate",
	)
	output, err := cmd.Output()
	if err != nil {
		// 尝试用 event=push 过滤
		cmd = exec.Command("gh", "run", "list",
			"--repo", repo,
			"--branch", tagName,
			"--json", "status,conclusion",
			"--limit", "1",
		)
		output, err = cmd.Output()
		if err != nil {
			return "", "", err
		}
	}

	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" || outputStr == "[]" {
		return "", "", fmt.Errorf("未找到相关的 workflow run")
	}

	// 解析 JSON
	var runs []struct {
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}

	// 处理可能的多行 JSON 或数组
	if strings.HasPrefix(outputStr, "[") {
		if err := json.Unmarshal([]byte(outputStr), &runs); err != nil {
			return "", "", fmt.Errorf("解析 workflow 状态失败: %w", err)
		}
	} else {
		// 单个 JSON 对象
		var run struct {
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		}
		if err := json.Unmarshal([]byte(outputStr), &run); err != nil {
			return "", "", fmt.Errorf("解析 workflow 状态失败: %w", err)
		}
		runs = append(runs, run)
	}

	if len(runs) == 0 {
		return "", "", fmt.Errorf("未找到相关的 workflow run")
	}

	return runs[0].Status, runs[0].Conclusion, nil
}

// checkReleaseExists 检查 Release 是否存在
func checkReleaseExists(repo, tagName string) (exists bool, url string, err error) {
	cmd := exec.Command("gh", "release", "view", tagName,
		"--repo", repo,
		"--json", "url",
		"-q", ".url",
	)
	output, err := cmd.Output()
	if err != nil {
		return false, "", nil // Release 不存在
	}
	return true, strings.TrimSpace(string(output)), nil
}
