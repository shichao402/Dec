package cmd

import (
	"fmt"

	"github.com/firoyang/CursorToolset/pkg/installer"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/firoyang/CursorToolset/pkg/registry"
	"github.com/spf13/cobra"
)

var (
	installNoCache bool
)

var installCmd = &cobra.Command{
	Use:   "install [package-name]",
	Short: "安装工具集包",
	Long: `安装一个或多个工具集包。

如果不指定包名，将安装所有可用的包。
如果指定了包名，只安装该包。

安装流程：
  1. 从 registry 获取包信息
  2. 下载包的 tarball 文件
  3. 验证 SHA256 校验和
  4. 解压到本地目录

示例：
  # 安装指定包
  cursortoolset install github-action-toolset

  # 安装所有可用包
  cursortoolset install

  # 不使用缓存安装
  cursortoolset install github-action-toolset --no-cache`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// 确保目录结构存在
		if err := paths.EnsureAllDirs(); err != nil {
			return fmt.Errorf("初始化目录失败: %w", err)
		}

		// 加载 registry
		mgr := registry.NewManager()
		if err := mgr.Load(); err != nil {
			return fmt.Errorf("加载包索引失败: %w", err)
		}

		// 检查是否有本地缓存
		if !mgr.HasLocalCache() {
			fmt.Println("📦 首次使用，正在更新包索引...")
			if err := mgr.Update(); err != nil {
				return fmt.Errorf("更新包索引失败: %w", err)
			}
		}

		// 创建安装器
		inst := installer.NewInstaller()
		inst.SetUseCache(!installNoCache)

		if len(args) > 0 {
			// 安装指定包
			return installPackage(mgr, inst, args[0])
		}

		// 安装所有包
		return installAllPackages(mgr, inst)
	},
}

func init() {
	installCmd.Flags().BoolVar(&installNoCache, "no-cache", false, "不使用缓存，强制重新下载")
}

// installPackage 安装指定包
func installPackage(mgr *registry.Manager, inst *installer.Installer, packageName string) error {
	// 查找包
	manifest := mgr.FindPackage(packageName)
	if manifest == nil {
		return fmt.Errorf("未找到包: %s\n\n提示: 运行 'cursortoolset registry update' 更新包索引", packageName)
	}

	// 检查是否已安装
	if inst.IsInstalled(packageName) {
		installedVer, _ := inst.GetInstalledVersion(packageName)
		if installedVer != "" && installedVer == manifest.Version {
			fmt.Printf("✅ %s@%s 已是最新版本\n", packageName, manifest.Version)
			return nil
		}
	}

	// 安装依赖
	if len(manifest.Dependencies) > 0 {
		fmt.Printf("📦 安装依赖...\n")
		for _, depName := range manifest.Dependencies {
			if inst.IsInstalled(depName) {
				fmt.Printf("  ✅ %s 已安装\n", depName)
				continue
			}

			depManifest := mgr.FindPackage(depName)
			if depManifest == nil {
				fmt.Printf("  ⚠️  未找到依赖: %s\n", depName)
				continue
			}

			if err := inst.Install(depManifest); err != nil {
				return fmt.Errorf("安装依赖 %s 失败: %w", depName, err)
			}
		}
		fmt.Println()
	}

	// 安装包
	return inst.Install(manifest)
}

// installAllPackages 安装所有包
func installAllPackages(mgr *registry.Manager, inst *installer.Installer) error {
	packages := mgr.ListPackages()
	if len(packages) == 0 {
		fmt.Println("📦 没有可用的包")
		fmt.Println("\n提示: 运行 'cursortoolset registry update' 更新包索引")
		return nil
	}

	fmt.Printf("📦 开始安装 %d 个包...\n\n", len(packages))

	installed := 0
	skipped := 0
	failed := 0

	for _, item := range packages {
		manifest := mgr.FindPackage(item.Name)
		if manifest == nil {
			fmt.Printf("⚠️  跳过 %s: 无法获取包信息\n", item.Name)
			skipped++
			continue
		}

		// 检查是否已安装最新版本
		if inst.IsInstalled(item.Name) {
			installedVer, _ := inst.GetInstalledVersion(item.Name)
			if installedVer == manifest.Version {
				fmt.Printf("⏭️  %s@%s 已安装\n", item.Name, manifest.Version)
				skipped++
				continue
			}
		}

		if err := inst.Install(manifest); err != nil {
			fmt.Printf("❌ 安装 %s 失败: %v\n", item.Name, err)
			failed++
			continue
		}

		installed++
		fmt.Println()
	}

	// 显示统计
	fmt.Println()
	fmt.Printf("📊 安装统计: 成功 %d, 跳过 %d", installed, skipped)
	if failed > 0 {
		fmt.Printf(", 失败 %d", failed)
	}
	fmt.Println()

	if failed > 0 {
		return fmt.Errorf("有 %d 个包安装失败", failed)
	}

	return nil
}

