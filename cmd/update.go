package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/firoyang/CursorToolset/pkg/loader"
	"github.com/firoyang/CursorToolset/pkg/version"
	"github.com/spf13/cobra"
)

var (
	updateSelf      bool
	updateToolsets  bool
	updateAvailable bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "更新 CursorToolset 或已安装的工具集",
	Long: `更新功能：
  1. --self: 更新 CursorToolset 本身
  2. --available: 更新 available-toolsets.json 配置文件
  3. --toolsets: 更新所有已安装的工具集
  
如果不指定任何参数，将执行所有更新。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 如果没有指定任何参数，则更新所有
		if !updateSelf && !updateToolsets && !updateAvailable {
			updateSelf = true
			updateToolsets = true
			updateAvailable = true
		}

		var hasError bool

		// 更新 CursorToolset 自身
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

		// 更新 available-toolsets.json
		if updateAvailable {
			fmt.Println("🔄 更新 available-toolsets.json...")
			if err := updateAvailableToolsets(); err != nil {
				fmt.Printf("❌ 更新失败: %v\n", err)
				hasError = true
			} else {
				fmt.Println("✅ available-toolsets.json 更新完成")
			}
			fmt.Println()
		}

		// 更新已安装的工具集
		if updateToolsets {
			fmt.Println("🔄 更新已安装的工具集...")
			if err := updateInstalledToolsets(); err != nil {
				fmt.Printf("❌ 更新失败: %v\n", err)
				hasError = true
			} else {
				fmt.Println("✅ 所有工具集更新完成")
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
	updateCmd.Flags().BoolVarP(&updateAvailable, "available", "a", false, "更新 available-toolsets.json")
	updateCmd.Flags().BoolVarP(&updateToolsets, "toolsets", "t", false, "更新已安装的工具集")
}

// updateSelfBinary 更新 CursorToolset 自身
func updateSelfBinary() error {
	// 检查是否有新版本
	fmt.Printf("  🔍 检查新版本...\n")
	
	// 从 version.json 读取当前版本
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取工作目录失败: %w", err)
	}
	
	currentVer, err := version.GetVersion(workDir)
	if err != nil {
		// 如果读取失败，使用编译时注入的版本
		currentVer = GetVersion()
		fmt.Printf("  ⚠️  无法读取 version.json，使用编译版本: %s\n", currentVer)
	}
	release, err := version.GetLatestRelease("firoyang", "CursorToolset")
	if err != nil {
		fmt.Printf("  ⚠️  无法检查版本: %v\n", err)
		fmt.Printf("  ℹ️  继续尝试更新...\n")
		// 继续执行更新
	} else {
		latestVer := release.TagName
		fmt.Printf("  📌 当前版本: %s\n", currentVer)
		fmt.Printf("  📌 最新版本: %s\n", latestVer)
		
		if !version.NeedUpdate(currentVer, latestVer) {
			fmt.Printf("  ✅ 已是最新版本，无需更新\n")
			return nil
		}
		
		fmt.Printf("  🆕 发现新版本！\n")
	}
	
	// 获取当前可执行文件路径
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

	// 检查是否是通过一键安装脚本安装的（在 ~/.cursor/toolsets/CursorToolset/ 下）
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}

	expectedDir := filepath.Join(homeDir, ".cursor", "toolsets", "CursorToolset", "bin")
	isStandardInstall := filepath.Clean(exeDir) == filepath.Clean(expectedDir)

	if !isStandardInstall {
		fmt.Printf("  ℹ️  检测到非标准安装位置\n")
		fmt.Printf("  ℹ️  标准位置: %s\n", expectedDir)
		fmt.Printf("  ℹ️  当前位置: %s\n", exeDir)
		
		// 询问用户是否继续
		fmt.Print("  ⚠️  继续更新可能需要手动处理。是否继续？[y/N]: ")
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

	// 处理 Windows 文件占用问题
	if runtime.GOOS == "windows" {
		return updateOnWindows(exePath, newBinaryPath)
	}

	// Unix-like 系统直接替换
	fmt.Printf("  📦 替换旧版本...\n")
	
	// 备份旧文件
	backupPath := exePath + ".backup"
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("备份旧文件失败: %w", err)
	}

	// 复制新文件
	if err := copyFile(newBinaryPath, exePath); err != nil {
		// 恢复备份
		os.Rename(backupPath, exePath)
		return fmt.Errorf("复制新文件失败: %w", err)
	}

	// 设置可执行权限
	if err := os.Chmod(exePath, 0755); err != nil {
		return fmt.Errorf("设置权限失败: %w", err)
	}

	// 删除备份
	os.Remove(backupPath)

	return nil
}

// updateOnWindows Windows 特殊处理
func updateOnWindows(oldPath, newPath string) error {
	fmt.Printf("  ⚠️  Windows 系统检测到文件可能被占用\n")
	
	// 创建更新脚本
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
	
	// 启动更新脚本
	cmd := exec.Command("cmd", "/c", "start", "/min", updateScript)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动更新脚本失败: %w", err)
	}

	// 退出当前程序
	os.Exit(0)
	return nil
}

// updateAvailableToolsets 更新 available-toolsets.json
func updateAvailableToolsets() error {
	// 查找 available-toolsets.json 位置
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取工作目录失败: %w", err)
	}

	toolsetsPath := loader.GetToolsetsPath(workDir)
	fmt.Printf("  📍 配置文件: %s\n", toolsetsPath)
	
	// 检查远程文件是否有更新
	fmt.Printf("  🔍 检查配置文件更新...\n")
	
	// 获取本地文件的修改时间
	localInfo, err := os.Stat(toolsetsPath)
	if err != nil && !os.IsNotExist(err) {
		fmt.Printf("  ⚠️  读取本地文件信息失败: %v\n", err)
	}

	// 检查是否是标准安装位置
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}

	standardPath := filepath.Join(homeDir, ".cursor", "toolsets", "CursorToolset", "available-toolsets.json")
	
	// 从 GitHub 下载最新版本
	fmt.Printf("  📥 下载最新配置...\n")
	
	tempFile := toolsetsPath + ".tmp"
	cmd := exec.Command("curl", "-fsSL", "-o", tempFile,
		"https://raw.githubusercontent.com/firoyang/CursorToolset/main/available-toolsets.json")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	
	// 检查文件是否有变化
	if localInfo != nil {
		// 比较文件内容
		oldContent, _ := os.ReadFile(toolsetsPath)
		newContent, _ := os.ReadFile(tempFile)
		
		if string(oldContent) == string(newContent) {
			os.Remove(tempFile)
			fmt.Printf("  ✅ 配置文件已是最新，无需更新\n")
			return nil
		}
	}

	// 替换旧文件
	if err := os.Rename(tempFile, toolsetsPath); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("替换文件失败: %w", err)
	}
	
	fmt.Printf("  ✅ 配置文件已更新\n")

	// 如果标准位置不同，也更新标准位置
	if filepath.Clean(toolsetsPath) != filepath.Clean(standardPath) {
		if err := copyFile(toolsetsPath, standardPath); err != nil {
			fmt.Printf("  ⚠️  更新标准位置失败: %v\n", err)
		}
	}

	return nil
}

// updateInstalledToolsets 更新已安装的工具集
func updateInstalledToolsets() error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取工作目录失败: %w", err)
	}

	// 加载工具集列表
	toolsetsPath := loader.GetToolsetsPath(workDir)
	toolsets, err := loader.LoadToolsets(toolsetsPath)
	if err != nil {
		return fmt.Errorf("加载工具集列表失败: %w", err)
	}

	// 查找已安装的工具集
	toolsetsDir := filepath.Join(workDir, ".cursor", "toolsets")
	
	updated := 0
	failed := 0

	for _, toolset := range toolsets {
		toolsetPath := filepath.Join(toolsetsDir, toolset.Name)
		
		// 检查是否已安装
		if _, err := os.Stat(toolsetPath); os.IsNotExist(err) {
			continue
		}

		fmt.Printf("  🔄 检查 %s...\n", toolset.DisplayName)
		
		// 先 fetch 检查是否有更新
		fetchCmd := exec.Command("git", "fetch")
		fetchCmd.Dir = toolsetPath
		if err := fetchCmd.Run(); err != nil {
			fmt.Printf("    ⚠️  检查更新失败: %v\n", err)
			failed++
			continue
		}
		
		// 检查是否有新的提交
		statusCmd := exec.Command("git", "status", "-uno")
		statusCmd.Dir = toolsetPath
		output, err := statusCmd.Output()
		if err != nil {
			fmt.Printf("    ⚠️  获取状态失败: %v\n", err)
			failed++
			continue
		}
		
		// 检查输出中是否包含 "Your branch is behind"
		statusStr := string(output)
		if !strings.Contains(statusStr, "Your branch is behind") {
			fmt.Printf("    ✅ 已是最新版本\n")
			continue
		}
		
		fmt.Printf("    🆕 发现新版本，正在更新...\n")

		// 拉取最新代码
		pullCmd := exec.Command("git", "pull")
		pullCmd.Dir = toolsetPath
		
		if err := pullCmd.Run(); err != nil {
			fmt.Printf("    ❌ 更新失败: %v\n", err)
			failed++
			continue
		}

		fmt.Printf("    ✅ 更新成功\n")
		updated++
	}

	if updated > 0 {
		fmt.Printf("\n  📊 更新统计: 成功 %d 个", updated)
		if failed > 0 {
			fmt.Printf(", 失败 %d 个", failed)
		}
		fmt.Println()
	} else {
		fmt.Println("  ℹ️  没有已安装的工具集")
	}

	if failed > 0 {
		return fmt.Errorf("有 %d 个工具集更新失败", failed)
	}

	return nil
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	// 确保目标目录存在
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	return os.WriteFile(dst, data, 0644)
}


