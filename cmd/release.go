package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var (
	releaseMajor   bool
	releaseMinor   bool
	releasePatch   bool
	releaseDryRun  bool
	releaseSkipTag bool
)

var releaseCmd = &cobra.Command{
	Use:   "release",
	Short: "发布新版本",
	Long: `发布新版本，自动完成以下步骤：

  1. 提升版本号（默认 patch）
  2. 打包并计算 SHA256
  3. 更新 toolset.json
  4. 创建 Git commit 和 tag
  5. 推送到远程仓库

示例：
  cursortoolset release              # 发布 patch 版本
  cursortoolset release --minor      # 发布 minor 版本
  cursortoolset release --major      # 发布 major 版本
  cursortoolset release --dry-run    # 预览发布流程，不执行`,
	RunE: runRelease,
}

func init() {
	releaseCmd.Flags().BoolVar(&releaseMajor, "major", false, "发布主版本 (x.0.0)")
	releaseCmd.Flags().BoolVar(&releaseMinor, "minor", false, "发布次版本 (0.x.0)")
	releaseCmd.Flags().BoolVar(&releasePatch, "patch", false, "发布补丁版本 (0.0.x)")
	releaseCmd.Flags().BoolVar(&releaseDryRun, "dry-run", false, "预览模式，不执行实际操作")
	releaseCmd.Flags().BoolVar(&releaseSkipTag, "skip-tag", false, "跳过 Git tag 和 push")
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

	// Step 1: 更新版本号
	fmt.Println("📝 Step 1: 更新版本号")
	if !releaseDryRun {
		if err := updateVersionInManifest(manifest, manifestPath, currentVersion, newVersion); err != nil {
			return fmt.Errorf("更新版本号失败: %w", err)
		}
	}
	fmt.Printf("   ✅ toolset.json 版本已更新为 %s\n\n", newVersion)

	// Step 2: 打包
	fmt.Println("📦 Step 2: 打包")
	outputFile := fmt.Sprintf("%s-%s.tar.gz", packageName, newVersion)
	if !releaseDryRun {
		// 直接调用 pack 逻辑
		packOutput = outputFile
		packVerify = true
		if err := runPack(nil, []string{"."}); err != nil {
			return fmt.Errorf("打包失败: %w", err)
		}
	} else {
		fmt.Printf("   ✅ 将创建 %s\n\n", outputFile)
	}

	// Step 3: Git commit (SHA256 已在 pack --verify 中更新)
	fmt.Println("📝 Step 3: Git commit")
	commitMsg := fmt.Sprintf("chore: release v%s", newVersion)
	if !releaseDryRun {
		if err := gitAdd("toolset.json"); err != nil {
			return fmt.Errorf("git add 失败: %w", err)
		}
		if err := gitCommit(commitMsg); err != nil {
			return fmt.Errorf("git commit 失败: %w", err)
		}
	}
	fmt.Printf("   ✅ 已提交: %s\n\n", commitMsg)

	// Step 4: Git tag
	if !releaseSkipTag {
		fmt.Println("🏷️  Step 4: Git tag")
		tagName := fmt.Sprintf("v%s", newVersion)
		if !releaseDryRun {
			if err := gitTag(tagName); err != nil {
				return fmt.Errorf("git tag 失败: %w", err)
			}
		}
		fmt.Printf("   ✅ 已创建标签: %s\n\n", tagName)

		// Step 5: Git push
		fmt.Println("🚀 Step 5: Git push")
		if !releaseDryRun {
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
		} else {
			fmt.Println("   ✅ 将推送到远程仓库")
		}
		fmt.Println()
	}

	// 完成
	fmt.Println("✅ 发布完成！")
	fmt.Println()
	fmt.Println("💡 下一步：")
	fmt.Printf("   1. 在 GitHub 创建 Release (v%s)\n", newVersion)
	fmt.Printf("   2. 上传 %s 到 Release\n", outputFile)
	if releaseSkipTag {
		fmt.Printf("   3. 创建并推送 Git tag:\n")
		fmt.Printf("      git tag v%s && git push --tags\n", newVersion)
	}

	return nil
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
