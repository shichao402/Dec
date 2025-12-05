package installer

import (
	"fmt"
	"os"

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
// 通过读取包目录中的 toolset.json 获取版本信息
func (i *Installer) GetInstalledVersion(packageName string) (string, error) {
	packagePath, err := paths.GetPackagePath(packageName)
	if err != nil {
		return "", err
	}

	// 检查包是否存在
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		return "", fmt.Errorf("包未安装")
	}

	// TODO: 读取 toolset.json 获取版本
	// 目前返回空字符串表示已安装但版本未知
	return "", nil
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
