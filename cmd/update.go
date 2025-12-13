package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/installer"
	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/registry"
	"github.com/shichao402/Dec/pkg/state"
	"github.com/spf13/cobra"
)

var (
	updateSelf     bool
	updateRegistry bool
	updatePackages bool
	updateYes      bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "更新管理器或已安装的包",
	Long: `更新功能：
  --self       更新 Dec 管理器本身
  --registry   更新包索引
  --packages   更新所有已安装的包
  --yes        跳过确认提示（适用于自动化/AI 辅助场景）
  
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
			fmt.Println("🔄 更新 Dec...")
			if err := updateSelfBinary(); err != nil {
				fmt.Printf("❌ 更新失败: %v\n", err)
				hasError = true
			} else {
				fmt.Println("✅ Dec 更新完成")
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
	updateCmd.Flags().BoolVarP(&updateSelf, "self", "s", false, "更新 Dec 本身")
	updateCmd.Flags().BoolVarP(&updateRegistry, "registry", "r", false, "更新包索引")
	updateCmd.Flags().BoolVarP(&updatePackages, "packages", "p", false, "更新已安装的包")
	updateCmd.Flags().BoolVarP(&updateYes, "yes", "y", false, "跳过确认提示")
}

// updateSelfBinary 更新管理器自身（从 GitHub Releases 下载预编译二进制）
func updateSelfBinary() error {
	// 获取当前版本
	currentVer := GetVersion()
	fmt.Printf("  📌 当前版本: %s\n", currentVer)

	// 开发版本不更新
	if currentVer == "dev" {
		fmt.Printf("  ℹ️  开发版本，跳过更新\n")
		return nil
	}

	// 获取最新版本号
	fmt.Printf("  🔍 检查最新版本...\n")
	latestVer, err := getLatestVersion()
	if err != nil {
		return fmt.Errorf("获取最新版本失败: %w", err)
	}
	fmt.Printf("  📌 最新版本: %s\n", latestVer)

	// 比较版本
	if currentVer == latestVer {
		fmt.Printf("  ✅ 已是最新版本\n")
		return nil
	}

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

		if !updateYes {
			fmt.Print("  ⚠️  继续更新？[y/N]: ")
			var response string
			_, _ = fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				return fmt.Errorf("用户取消更新")
			}
		} else {
			fmt.Printf("  ⚠️  --yes 模式，自动继续\n")
		}
	}

	// 构建下载 URL
	platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	binaryName := fmt.Sprintf("dec-%s", platform)
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	cfg := config.GetSystemConfig()
	downloadURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		cfg.RepoOwner, cfg.RepoName, latestVer, binaryName)

	fmt.Printf("  📥 下载新版本...\n")
	fmt.Printf("  📡 下载地址: %s\n", downloadURL)

	// 创建临时文件
	tempFile, err := os.CreateTemp("", "dec-update-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		tempFile.Close()
		os.Remove(tempPath)
	}()

	// 下载新版本
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d - 请确认版本 %s 已发布", resp.StatusCode, latestVer)
	}

	_, err = io.Copy(tempFile, resp.Body)
	if err != nil {
		return fmt.Errorf("保存文件失败: %w", err)
	}
	tempFile.Close()

	// Windows 特殊处理
	if runtime.GOOS == "windows" {
		return updateOnWindows(exePath, tempPath)
	}

	// Unix 系统直接替换
	fmt.Printf("  📦 替换旧版本...\n")

	backupPath := exePath + ".backup"
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("备份旧文件失败: %w", err)
	}

	if err := copyFile(tempPath, exePath); err != nil {
		_ = os.Rename(backupPath, exePath)
		return fmt.Errorf("复制新文件失败: %w", err)
	}

	if err := os.Chmod(exePath, 0755); err != nil {
		return fmt.Errorf("设置权限失败: %w", err)
	}

	_ = os.Remove(backupPath)

	fmt.Printf("  ✅ 更新成功: %s -> %s\n", currentVer, latestVer)

	// 写入版本状态，触发下次命令执行时自动更新文档
	if err := state.SetVersion(latestVer); err != nil {
		fmt.Printf("  ⚠️  写入版本状态失败: %v\n", err)
	}

	return nil
}

// getLatestVersion 从更新分支获取最新版本号
func getLatestVersion() (string, error) {
	versionURL := config.GetVersionURL()

	resp, err := http.Get(versionURL)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var versionInfo struct {
		Version string `json:"version"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&versionInfo); err != nil {
		return "", fmt.Errorf("解析版本信息失败: %w", err)
	}

	return versionInfo.Version, nil
}

// updateOnWindows Windows 特殊处理
func updateOnWindows(oldPath, newPath string) error {
	fmt.Printf("  ⚠️  Windows 系统检测到文件可能被占用\n")

	updateScript := filepath.Join(filepath.Dir(oldPath), "update-dec.bat")

	scriptContent := fmt.Sprintf(`@echo off
echo Waiting for dec to exit...
timeout /t 2 /nobreak >nul
echo Updating dec...
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
		repoName := item.GetRepoName()
		manifest := mgr.GetManifestByRepo(repoName)
		if manifest == nil {
			continue
		}

		// 检查是否已安装
		if !inst.IsInstalled(manifest.Name) {
			continue
		}

		// 检查版本
		installedVer, _ := inst.GetInstalledVersion(manifest.Name)
		if installedVer == manifest.Version {
			fmt.Printf("  ✅ %s@%s 已是最新\n", manifest.Name, manifest.Version)
			skipped++
			continue
		}

		fmt.Printf("  🔄 更新 %s -> %s\n", manifest.Name, manifest.Version)
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
