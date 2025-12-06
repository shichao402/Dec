package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "版本管理命令",
	Long: `版本管理命令，用于查看和修改包的版本号。

子命令：
  bump    提升版本号 (patch/minor/major)
  set     设置指定版本号

示例：
  cursortoolset version bump --patch   # 1.0.0 -> 1.0.1
  cursortoolset version bump --minor   # 1.0.1 -> 1.1.0
  cursortoolset version bump --major   # 1.1.0 -> 2.0.0
  cursortoolset version set 2.0.0      # 直接设置版本`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 默认显示当前版本
		return showCurrentVersion()
	},
}

var (
	bumpMajor bool
	bumpMinor bool
	bumpPatch bool
)

var versionBumpCmd = &cobra.Command{
	Use:   "bump",
	Short: "提升版本号",
	Long: `提升版本号，支持 major、minor、patch 三种方式。

示例：
  cursortoolset version bump --patch   # 1.0.0 -> 1.0.1
  cursortoolset version bump --minor   # 1.0.1 -> 1.1.0
  cursortoolset version bump --major   # 1.1.0 -> 2.0.0`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 确定提升类型
		bumpType := ""
		count := 0
		if bumpMajor {
			bumpType = "major"
			count++
		}
		if bumpMinor {
			bumpType = "minor"
			count++
		}
		if bumpPatch {
			bumpType = "patch"
			count++
		}

		if count == 0 {
			bumpType = "patch" // 默认 patch
		} else if count > 1 {
			return fmt.Errorf("只能指定一种版本提升类型")
		}

		return bumpVersion(bumpType)
	},
}

var versionSetCmd = &cobra.Command{
	Use:   "set <version>",
	Short: "设置指定版本号",
	Long: `直接设置指定的版本号。

示例：
  cursortoolset version set 2.0.0`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		newVersion := args[0]

		// 验证版本号格式
		if !isValidVersion(newVersion) {
			return fmt.Errorf("版本号格式不正确，应为 MAJOR.MINOR.PATCH 格式，例如 1.0.0")
		}

		return setVersion(newVersion)
	},
}

func init() {
	versionBumpCmd.Flags().BoolVar(&bumpMajor, "major", false, "提升主版本号 (x.0.0)")
	versionBumpCmd.Flags().BoolVar(&bumpMinor, "minor", false, "提升次版本号 (0.x.0)")
	versionBumpCmd.Flags().BoolVar(&bumpPatch, "patch", false, "提升补丁版本号 (0.0.x)")

	versionCmd.AddCommand(versionBumpCmd)
	versionCmd.AddCommand(versionSetCmd)
	RootCmd.AddCommand(versionCmd)
}

// showCurrentVersion 显示当前版本
func showCurrentVersion() error {
	manifest, _, err := loadManifest()
	if err != nil {
		return err
	}

	fmt.Printf("📦 %s\n", manifest["name"])
	fmt.Printf("   版本: %s\n", manifest["version"])

	return nil
}

// bumpVersion 提升版本号
func bumpVersion(bumpType string) error {
	manifest, manifestPath, err := loadManifest()
	if err != nil {
		return err
	}

	currentVersion, ok := manifest["version"].(string)
	if !ok || currentVersion == "" {
		return fmt.Errorf("package.json 中缺少 version 字段")
	}

	// 解析版本号
	parts := strings.Split(currentVersion, ".")
	if len(parts) != 3 {
		return fmt.Errorf("当前版本号格式不正确: %s", currentVersion)
	}

	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	patch, _ := strconv.Atoi(parts[2])

	// 提升版本号
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

	newVersion := fmt.Sprintf("%d.%d.%d", major, minor, patch)

	fmt.Printf("📦 %s\n", manifest["name"])
	fmt.Printf("   %s -> %s\n\n", currentVersion, newVersion)

	// 更新版本号
	return updateVersion(manifest, manifestPath, currentVersion, newVersion)
}

// setVersion 设置版本号
func setVersion(newVersion string) error {
	manifest, manifestPath, err := loadManifest()
	if err != nil {
		return err
	}

	currentVersion, _ := manifest["version"].(string)

	fmt.Printf("📦 %s\n", manifest["name"])
	fmt.Printf("   %s -> %s\n\n", currentVersion, newVersion)

	return updateVersion(manifest, manifestPath, currentVersion, newVersion)
}

// updateVersion 更新版本号并保存
func updateVersion(manifest map[string]interface{}, manifestPath, oldVersion, newVersion string) error {
	// 更新 version 字段
	manifest["version"] = newVersion

	// 更新 dist.tarball 中的版本号
	if dist, ok := manifest["dist"].(map[string]interface{}); ok {
		if tarball, ok := dist["tarball"].(string); ok {
			// 替换旧版本号为新版本号
			newTarball := strings.ReplaceAll(tarball, oldVersion, newVersion)
			newTarball = strings.ReplaceAll(newTarball, "v"+oldVersion, "v"+newVersion)
			dist["tarball"] = newTarball
		}
		// 清空 sha256（需要重新计算）
		dist["sha256"] = "TODO: 运行 cursortoolset pack --verify 更新"
	}

	// 保存文件
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}

	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	fmt.Println("✅ 版本号已更新")
	fmt.Println("\n💡 下一步：")
	fmt.Println("   1. 运行 cursortoolset pack --verify 打包并更新 SHA256")
	fmt.Println("   2. 提交更改并创建 Git Tag")
	fmt.Printf("      git add package.json && git commit -m \"chore: bump version to %s\"\n", newVersion)
	fmt.Printf("      git tag v%s\n", newVersion)

	return nil
}

// loadManifest 加载 package.json
func loadManifest() (map[string]interface{}, string, error) {
	// 查找 package.json
	manifestPath := filepath.Join(".", "package.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return nil, "", fmt.Errorf("当前目录不是工具集包项目（缺少 package.json）")
	}

	// 读取文件
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, "", fmt.Errorf("读取 package.json 失败: %w", err)
	}

	// 解析 JSON
	var manifest map[string]interface{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, "", fmt.Errorf("解析 package.json 失败: %w", err)
	}

	return manifest, manifestPath, nil
}
