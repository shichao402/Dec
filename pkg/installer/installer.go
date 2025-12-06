package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/firoyang/CursorToolset/pkg/downloader"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/firoyang/CursorToolset/pkg/types"
)

// Installer 负责安装和卸载包
type Installer struct {
	downloader *downloader.Downloader
	useCache   bool
}

// NewInstaller 创建新的安装器
func NewInstaller() *Installer {
	return &Installer{
		downloader: downloader.NewDownloader(),
		useCache:   true,
	}
}

// SetUseCache 设置是否使用缓存
func (i *Installer) SetUseCache(use bool) {
	i.useCache = use
	i.downloader.SetUseCache(use)
}

// Install 安装包
// 流程：下载 tarball → 验证 SHA256 → 解压到 repos 目录
func (i *Installer) Install(manifest *types.Manifest) error {
	fmt.Printf("📦 安装 %s@%s\n", manifest.Name, manifest.Version)

	// 检查是否已安装
	packagePath, err := paths.GetPackagePath(manifest.Name)
	if err != nil {
		return fmt.Errorf("获取包路径失败: %w", err)
	}

	if _, err := os.Stat(packagePath); err == nil {
		// 已安装，检查版本
		fmt.Printf("  ℹ️  包已安装，将更新到 %s\n", manifest.Version)
		// 删除旧版本
		if err := os.RemoveAll(packagePath); err != nil {
			return fmt.Errorf("删除旧版本失败: %w", err)
		}
	}

	// 确保 repos 目录存在
	reposDir, err := paths.GetReposDir()
	if err != nil {
		return fmt.Errorf("获取 repos 目录失败: %w", err)
	}
	if err := paths.EnsureDir(reposDir); err != nil {
		return fmt.Errorf("创建 repos 目录失败: %w", err)
	}

	// 下载并解压
	err = i.downloader.DownloadAndExtract(
		manifest.Dist.Tarball,
		manifest.Name,
		manifest.Version,
		manifest.Dist.SHA256,
		packagePath,
	)
	if err != nil {
		return fmt.Errorf("下载安装失败: %w", err)
	}

	fmt.Printf("✅ %s 安装完成\n", manifest.Name)
	
	// 创建可执行程序的符号链接
	if err := i.linkBinaries(manifest, packagePath); err != nil {
		fmt.Printf("  ⚠️  创建可执行程序链接失败: %v\n", err)
		// 不返回错误，让安装继续
	}
	
	// 友好提示：如何使用规则文件
	printInstallTip(packagePath, manifest.Name)
	
	return nil
}

// Uninstall 卸载包
func (i *Installer) Uninstall(packageName string) error {
	fmt.Printf("🗑️  卸载 %s\n", packageName)

	packagePath, err := paths.GetPackagePath(packageName)
	if err != nil {
		return fmt.Errorf("获取包路径失败: %w", err)
	}

	// 检查是否已安装
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		fmt.Printf("  ℹ️  包未安装\n")
		return nil
	}

	// 读取包的 manifest 以获取 bin 配置
	manifest, err := i.loadPackageManifest(packagePath)
	if err == nil && manifest != nil {
		// 清理可执行程序的符号链接
		if err := i.unlinkBinaries(manifest); err != nil {
			fmt.Printf("  ⚠️  清理可执行程序链接失败: %v\n", err)
			// 继续执行，不阻断卸载流程
		}
	}

	// 删除包目录
	if err := os.RemoveAll(packagePath); err != nil {
		return fmt.Errorf("删除包失败: %w", err)
	}

	fmt.Printf("✅ %s 卸载完成\n", packageName)
	return nil
}

// printInstallTip 打印安装后的使用提示
func printInstallTip(packagePath, packageName string) {
	// 检查是否有 rules 目录
	rulesPath := packagePath + "/rules"
	if _, err := os.Stat(rulesPath); err == nil {
		fmt.Printf("\n💡 使用提示:\n")
		fmt.Printf("   链接规则文件到项目:\n")
		fmt.Printf("   mkdir -p .cursor/rules\n")
		fmt.Printf("   ln -sf %s .cursor/rules/%s\n", rulesPath, packageName)
		fmt.Printf("\n   详细文档: https://github.com/firoyang/CursorToolset/blob/main/USAGE_EXAMPLE.md\n")
	}
}

// linkBinaries 为包中配置的可执行程序创建符号链接到 bin 目录
func (i *Installer) linkBinaries(manifest *types.Manifest, packagePath string) error {
	if len(manifest.Bin) == 0 {
		return nil
	}

	binDir, err := paths.GetBinDir()
	if err != nil {
		return err
	}

	// 确保 bin 目录存在
	if err := paths.EnsureDir(binDir); err != nil {
		return err
	}

	fmt.Printf("  🔗 创建可执行程序链接...\n")

	// 当前平台
	currentPlatform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)

	for cmdName, binConfig := range manifest.Bin {
		// 解析 bin 配置，支持两种格式
		relPath, err := i.resolveBinPath(binConfig, currentPlatform)
		if err != nil {
			fmt.Printf("    ⚠️  跳过 %s: %v\n", cmdName, err)
			continue
		}

		// 源文件（包中的可执行程序）
		srcPath := filepath.Join(packagePath, relPath)

		// 检查源文件是否存在
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			fmt.Printf("    ⚠️  跳过 %s: 文件不存在 (%s)\n", cmdName, relPath)
			continue
		}

		// 目标链接路径
		linkPath := filepath.Join(binDir, cmdName)

		// Windows 平台处理
		if runtime.GOOS == "windows" {
			// Windows 使用 .exe 扩展名
			if filepath.Ext(cmdName) != ".exe" {
				linkPath += ".exe"
			}
			if filepath.Ext(srcPath) != ".exe" {
				srcPath += ".exe"
			}
		}

		// 如果链接已存在，先删除
		if _, err := os.Lstat(linkPath); err == nil {
			if err := os.Remove(linkPath); err != nil {
				fmt.Printf("    ⚠️  无法删除旧链接 %s: %v\n", cmdName, err)
				continue
			}
		}

		// 创建符号链接
		if err := os.Symlink(srcPath, linkPath); err != nil {
			fmt.Printf("    ⚠️  无法创建链接 %s: %v\n", cmdName, err)
			continue
		}

		// 确保源文件可执行（Unix 系统）
		if runtime.GOOS != "windows" {
			if err := os.Chmod(srcPath, 0755); err != nil {
				fmt.Printf("    ⚠️  无法设置执行权限 %s: %v\n", cmdName, err)
			}
		}

		fmt.Printf("    ✅ %s -> %s\n", cmdName, relPath)
	}

	// 打印 PATH 提示
	fmt.Printf("\n  💡 将 bin 目录添加到 PATH:\n")
	if runtime.GOOS == "windows" {
		fmt.Printf("    set PATH=%s;%%PATH%%\n", binDir)
	} else {
		fmt.Printf("    export PATH=\"%s:$PATH\"\n", binDir)
	}
	fmt.Println()

	return nil
}

// resolveBinPath 解析 bin 配置，返回当前平台对应的路径
// 支持两种格式：
// 1. 简单格式（字符串）: "path/to/binary"
// 2. 多平台格式（对象）: {"darwin-arm64": "path/to/binary-darwin-arm64", ...}
func (i *Installer) resolveBinPath(binConfig interface{}, currentPlatform string) (string, error) {
	switch v := binConfig.(type) {
	case string:
		// 简单格式：直接返回路径
		return v, nil
	case map[string]interface{}:
		// 多平台格式：查找当前平台
		if path, ok := v[currentPlatform]; ok {
			if pathStr, ok := path.(string); ok {
				return pathStr, nil
			}
		}
		// 列出支持的平台
		var supported []string
		for platform := range v {
			supported = append(supported, platform)
		}
		return "", fmt.Errorf("当前平台 %s 不支持，支持的平台: %v", currentPlatform, supported)
	default:
		return "", fmt.Errorf("无效的 bin 配置格式")
	}
}

// unlinkBinaries 清理包中配置的可执行程序的符号链接
func (i *Installer) unlinkBinaries(manifest *types.Manifest) error {
	if len(manifest.Bin) == 0 {
		return nil
	}

	binDir, err := paths.GetBinDir()
	if err != nil {
		return err
	}

	fmt.Printf("  🔗 清理可执行程序链接...\n")

	for cmdName := range manifest.Bin {
		linkPath := filepath.Join(binDir, cmdName)

		// Windows 平台处理
		if runtime.GOOS == "windows" && filepath.Ext(cmdName) != ".exe" {
			linkPath += ".exe"
		}

		// 检查链接是否存在
		if _, err := os.Lstat(linkPath); os.IsNotExist(err) {
			continue
		}

		// 删除符号链接
		if err := os.Remove(linkPath); err != nil {
			fmt.Printf("    ⚠️  无法删除链接 %s: %v\n", cmdName, err)
			continue
		}

		fmt.Printf("    ✅ 已删除 %s\n", cmdName)
	}

	return nil
}

// loadPackageManifest 从已安装的包中加载 manifest
func (i *Installer) loadPackageManifest(packagePath string) (*types.Manifest, error) {
	manifestPath := filepath.Join(packagePath, "toolset.json")
	
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}

	var manifest types.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// IsInstalled 检查包是否已安装
func (i *Installer) IsInstalled(packageName string) bool {
	packagePath, err := paths.GetPackagePath(packageName)
	if err != nil {
		return false
	}
	_, err = os.Stat(packagePath)
	return err == nil
}

// GetInstalledVersion 获取已安装包的版本
// 通过读取包目录中的 package.json 或 toolset.json 获取版本信息
func (i *Installer) GetInstalledVersion(packageName string) (string, error) {
	packagePath, err := paths.GetPackagePath(packageName)
	if err != nil {
		return "", err
	}

	// 检查包是否存在
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		return "", fmt.Errorf("包未安装")
	}

	// 尝试读取 package.json 或 toolset.json
	var data []byte
	packageJSONPath := filepath.Join(packagePath, "package.json")
	toolsetJSONPath := filepath.Join(packagePath, "toolset.json")

	if d, err := os.ReadFile(packageJSONPath); err == nil {
		data = d
	} else if d, err := os.ReadFile(toolsetJSONPath); err == nil {
		data = d
	} else {
		return "", fmt.Errorf("未找到 package.json 或 toolset.json")
	}

	var pkgInfo struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkgInfo); err != nil {
		return "", fmt.Errorf("解析版本信息失败: %w", err)
	}

	return pkgInfo.Version, nil
}

// ClearCache 清理下载缓存
func (i *Installer) ClearCache() error {
	cacheDir, err := paths.GetPackageCacheDir()
	if err != nil {
		return err
	}

	return os.RemoveAll(cacheDir)
}

// ========================================
// 兼容旧版本的方法（逐步废弃）
// ========================================

// NewInstallerCompat 创建兼容旧版本的安装器
// Deprecated: 使用 NewInstaller 替代
func NewInstallerCompat(toolsetsDir, workDir string) *InstallerCompat {
	return &InstallerCompat{
		ToolsetsDir: toolsetsDir,
		WorkDir:     workDir,
		installer:   NewInstaller(),
	}
}

// InstallerCompat 兼容旧版本的安装器
// Deprecated: 使用 Installer 替代
type InstallerCompat struct {
	ToolsetsDir string
	WorkDir     string
	Version     string
	installer   *Installer
}

// SetVersion 设置版本（兼容旧接口）
func (i *InstallerCompat) SetVersion(version string) {
	i.Version = version
}

// InstallToolset 安装工具集（兼容旧接口）
func (i *InstallerCompat) InstallToolset(toolsetInfo *types.ToolsetInfo) error {
	// 转换为新的 Manifest 格式
	manifest := &types.Manifest{
		Name:        toolsetInfo.Name,
		DisplayName: toolsetInfo.DisplayName,
		Version:     toolsetInfo.Version,
		Description: toolsetInfo.Description,
	}

	// 如果有 ManifestURL，尝试获取完整信息
	// 否则使用旧的 GitHubURL（不支持新安装方式）
	if toolsetInfo.ManifestURL == "" && toolsetInfo.GitHubURL != "" {
		return fmt.Errorf("旧版本包格式不再支持，请更新包到新格式")
	}

	return i.installer.Install(manifest)
}

// UninstallToolset 卸载工具集（兼容旧接口）
func (i *InstallerCompat) UninstallToolset(toolsetInfo *types.ToolsetInfo) error {
	return i.installer.Uninstall(toolsetInfo.Name)
}
