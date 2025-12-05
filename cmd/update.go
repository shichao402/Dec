package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/firoyang/CursorToolset/pkg/installer"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/firoyang/CursorToolset/pkg/registry"
	"github.com/firoyang/CursorToolset/pkg/version"
	"github.com/spf13/cobra"
)

var (
	updateSelf     bool
	updateRegistry bool
	updatePackages bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "更新管理器或已安装的包",
	Long: `更新功能：
  --self       更新 CursorToolset 管理器本身
  --registry   更新包索引
  --packages   更新所有已安装的包
  
如果不指定任何参数，将执行所有更新。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 如果没有指定任何参数，则更新所有
		if !updateSelf && !updateRegistry && !updatePackages {
			updateSelf = true
			updateRegistry = true
			updatePackages = true
		}

		var hasError bool

		// 更新管理器自身
		if updateSelf {
			fmt.Println("🔄 更新 CursorToolset...")
			if err := updateSelfBinary(); err != nil {
				fmt.Printf("❌ 更新失败: %v\n", err)
				hasError = true
			} else {
				fmt.Println("✅ CursorToolset 更新完成")
			}
			fmt.Println()
		}

		// 更新 registry
		if updateRegistry {
			fmt.Println("🔄 更新包索引...")
			mgr := registry.NewManager()
			if err := mgr.Update(); err != nil {
				fmt.Printf("❌ 更新失败: %v\n", err)
				hasError = true
			}
			fmt.Println()
		}

		// 更新已安装的包
		if updatePackages {
			fmt.Println("🔄 更新已安装的包...")
			if err := updateInstalledPackages(); err != nil {
				fmt.Printf("❌ 更新失败: %v\n", err)
				hasError = true
			}
		}

		if hasError {
			return fmt.Errorf("部分更新失败，请查看上面的错误信息")
		}

		fmt.Println("🎉 所有更新完成！")
		return nil
	},
}

func init() {
	updateCmd.Flags().BoolVarP(&updateSelf, "self", "s", false, "更新 CursorToolset 本身")
	updateCmd.Flags().BoolVarP(&updateRegistry, "registry", "r", false, "更新包索引")
	updateCmd.Flags().BoolVarP(&updatePackages, "packages", "p", false, "更新已安装的包")
}

// updateSelfBinary 更新管理器自身
func updateSelfBinary() error {
	// 获取当前版本
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取工作目录失败: %w", err)
	}

	currentVer, err := version.GetVersion(workDir)
	if err != nil {
		currentVer = GetVersion()
		fmt.Printf("  ⚠️  无法读取版本信息，使用: %s\n", currentVer)
	}

	fmt.Printf("  📌 当前版本: %s\n", currentVer)
	fmt.Printf("  🔄 开始更新...\n")

	// 获取可执行文件路径
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("解析符号链接失败: %w", err)
	}

	exeDir := filepath.Dir(exePath)
	fmt.Printf("  📍 当前位置: %s\n", exePath)

	// 检查是否是标准安装位置
	expectedBinDir, err := paths.GetBinDir()
	if err != nil {
		return fmt.Errorf("获取标准安装目录失败: %w", err)
	}

	isStandardInstall := filepath.Clean(exeDir) == filepath.Clean(expectedBinDir)

	if !isStandardInstall {
		fmt.Printf("  ℹ️  检测到非标准安装位置\n")
		fmt.Printf("  ℹ️  标准位置: %s\n", expectedBinDir)
		fmt.Printf("  ℹ️  当前位置: %s\n", exeDir)

		fmt.Print("  ⚠️  继续更新？[y/N]: ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			return fmt.Errorf("用户取消更新")
		}
	}

	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "cursortoolset-update-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tempDir)

	fmt.Printf("  📥 克隆最新代码...\n")

	// 克隆最新代码
	cmd := exec.Command("git", "clone", "--depth", "1",
		"https://github.com/firoyang/CursorToolset.git", tempDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("克隆仓库失败: %w", err)
	}

	fmt.Printf("  🔨 构建新版本...\n")

	// 构建新版本
	newBinaryPath := filepath.Join(tempDir, "cursortoolset")
	if runtime.GOOS == "windows" {
		newBinaryPath += ".exe"
	}

	buildCmd := exec.Command("go", "build", "-o", newBinaryPath, ".")
	buildCmd.Dir = tempDir
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("构建失败: %w", err)
	}

	// Windows 特殊处理
	if runtime.GOOS == "windows" {
		return updateOnWindows(exePath, newBinaryPath)
	}

	// Unix 系统直接替换
	fmt.Printf("  📦 替换旧版本...\n")

	backupPath := exePath + ".backup"
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("备份旧文件失败: %w", err)
	}

	if err := copyFile(newBinaryPath, exePath); err != nil {
		os.Rename(backupPath, exePath)
		return fmt.Errorf("复制新文件失败: %w", err)
	}

	if err := os.Chmod(exePath, 0755); err != nil {
		return fmt.Errorf("设置权限失败: %w", err)
	}

	os.Remove(backupPath)
	return nil
}

// updateOnWindows Windows 特殊处理
func updateOnWindows(oldPath, newPath string) error {
	fmt.Printf("  ⚠️  Windows 系统检测到文件可能被占用\n")

	updateScript := filepath.Join(filepath.Dir(oldPath), "update-cursortoolset.bat")

	scriptContent := fmt.Sprintf(`@echo off
echo Waiting for cursortoolset to exit...
timeout /t 2 /nobreak >nul
echo Updating cursortoolset...
move /y "%s" "%s.backup" >nul 2>&1
move /y "%s" "%s"
if %%errorlevel%% equ 0 (
    echo Update successful!
    del "%s.backup" >nul 2>&1
    del "%%~f0"
) else (
    echo Update failed, restoring backup...
    move /y "%s.backup" "%s"
    pause
)
`, oldPath, oldPath, newPath, oldPath, oldPath, oldPath, oldPath)

	if err := os.WriteFile(updateScript, []byte(scriptContent), 0644); err != nil {
		return fmt.Errorf("创建更新脚本失败: %w", err)
	}

	fmt.Printf("  📝 已创建更新脚本: %s\n", updateScript)
	fmt.Printf("  ℹ️  程序将退出并自动完成更新\n")

	cmd := exec.Command("cmd", "/c", "start", "/min", updateScript)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动更新脚本失败: %w", err)
	}

	os.Exit(0)
	return nil
}

// updateInstalledPackages 更新已安装的包
func updateInstalledPackages() error {
	// 确保目录结构存在
	if err := paths.EnsureAllDirs(); err != nil {
		return fmt.Errorf("初始化目录失败: %w", err)
	}

	// 加载 registry
	mgr := registry.NewManager()
	if err := mgr.Load(); err != nil {
		return fmt.Errorf("加载包索引失败: %w", err)
	}

	inst := installer.NewInstaller()
	packages := mgr.ListPackages()

	updated := 0
	skipped := 0
	failed := 0

	for _, item := range packages {
		// 检查是否已安装
		if !inst.IsInstalled(item.Name) {
			continue
		}

		manifest := mgr.FindPackage(item.Name)
		if manifest == nil {
			fmt.Printf("  ⚠️  跳过 %s: 无法获取包信息\n", item.Name)
			skipped++
			continue
		}

		// 检查版本
		installedVer, _ := inst.GetInstalledVersion(item.Name)
		if installedVer == manifest.Version {
			fmt.Printf("  ✅ %s@%s 已是最新\n", item.Name, manifest.Version)
			skipped++
			continue
		}

		fmt.Printf("  🔄 更新 %s -> %s\n", item.Name, manifest.Version)
		if err := inst.Install(manifest); err != nil {
			fmt.Printf("  ❌ 更新失败: %v\n", err)
			failed++
			continue
		}

		updated++
	}

	fmt.Printf("\n📊 更新统计: 更新 %d, 跳过 %d", updated, skipped)
	if failed > 0 {
		fmt.Printf(", 失败 %d", failed)
	}
	fmt.Println()

	if failed > 0 {
		return fmt.Errorf("有 %d 个包更新失败", failed)
	}

	return nil
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	return os.WriteFile(dst, data, 0644)
}
