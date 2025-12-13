package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/shichao402/Dec/pkg/downloader"
	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/types"
)

// MCPInstaller MCP 工具包安装器
type MCPInstaller struct {
	downloader *downloader.Downloader
	useCache   bool
}

// NewMCPInstaller 创建 MCP 安装器
func NewMCPInstaller() *MCPInstaller {
	return &MCPInstaller{
		downloader: downloader.NewDownloader(),
		useCache:   true,
	}
}

// SetUseCache 设置是否使用缓存
func (i *MCPInstaller) SetUseCache(use bool) {
	i.useCache = use
	i.downloader.SetUseCache(use)
}

// InstallPack 安装包（规则包或 MCP 包）
func (i *MCPInstaller) InstallPack(pack *types.Pack, tarballURL string, sha256 string) error {
	fmt.Printf("📦 安装 %s@%s (%s)\n", pack.Name, pack.Version, pack.Type)

	// 确定安装路径
	var installPath string
	var err error

	switch pack.Type {
	case types.PackTypeMCP:
		installPath, err = paths.GetMCPPackPath(pack.Name)
	case types.PackTypeRule:
		installPath, err = paths.GetRulePackPath(pack.Name)
	default:
		return fmt.Errorf("未知的包类型: %s", pack.Type)
	}

	if err != nil {
		return fmt.Errorf("获取安装路径失败: %w", err)
	}

	// 检查是否已安装
	if _, err := os.Stat(installPath); err == nil {
		fmt.Printf("  ℹ️  包已安装，将更新到 %s\n", pack.Version)
		if err := os.RemoveAll(installPath); err != nil {
			return fmt.Errorf("删除旧版本失败: %w", err)
		}
	}

	// 确保父目录存在
	if err := paths.EnsureDir(filepath.Dir(installPath)); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 下载并解压
	err = i.downloader.DownloadAndExtract(
		tarballURL,
		pack.Name,
		pack.Version,
		sha256,
		installPath,
	)
	if err != nil {
		return fmt.Errorf("下载安装失败: %w", err)
	}

	// 对于 MCP 包，选择当前平台的可执行文件
	if pack.Type == types.PackTypeMCP {
		if err := i.selectPlatformBinary(installPath); err != nil {
			fmt.Printf("  ⚠️  选择平台可执行文件失败: %v\n", err)
		}
	}

	fmt.Printf("✅ %s 安装完成\n", pack.Name)
	return nil
}

// selectPlatformBinary 选择当前平台的可执行文件
// 从 bin/ 目录中选择匹配当前平台的文件，并重命名
func (i *MCPInstaller) selectPlatformBinary(installPath string) error {
	binDir := filepath.Join(installPath, "bin")
	if _, err := os.Stat(binDir); os.IsNotExist(err) {
		return nil // 没有 bin 目录，跳过
	}

	// 当前平台标识
	platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)

	entries, err := os.ReadDir(binDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// 查找匹配当前平台的文件
		// 格式: name-darwin-arm64, name-linux-amd64 等
		if containsPlatform(name, platform) {
			// 提取基础名称
			baseName := extractBaseName(name, platform)
			if baseName == "" {
				continue
			}

			srcPath := filepath.Join(binDir, name)
			dstPath := filepath.Join(binDir, baseName)

			// 如果目标文件已存在且不同，先删除
			if srcPath != dstPath {
				if _, err := os.Stat(dstPath); err == nil {
					os.Remove(dstPath)
				}
				// 重命名（或创建符号链接）
				if err := os.Rename(srcPath, dstPath); err != nil {
					return err
				}
			}

			// 设置可执行权限
			if runtime.GOOS != "windows" {
				os.Chmod(dstPath, 0755)
			}

			fmt.Printf("  🔧 选择平台可执行文件: %s\n", baseName)
		}
	}

	return nil
}

// containsPlatform 检查文件名是否包含平台标识
func containsPlatform(filename, platform string) bool {
	// 检查常见格式
	// name-darwin-arm64
	// name_darwin_arm64
	// name.darwin.arm64
	return filepath.Base(filename) != "" &&
		(contains(filename, "-"+platform) ||
			contains(filename, "_"+platform) ||
			contains(filename, "."+platform))
}

// extractBaseName 从平台特定文件名中提取基础名称
func extractBaseName(filename, platform string) string {
	// 移除平台后缀
	base := filename
	for _, sep := range []string{"-", "_", "."} {
		suffix := sep + platform
		if idx := lastIndex(base, suffix); idx > 0 {
			base = base[:idx]
			break
		}
	}
	// 移除 .exe 扩展名（如果有）
	base = trimSuffix(base, ".exe")
	return base
}

// UninstallPack 卸载包
func (i *MCPInstaller) UninstallPack(name string, packType string) error {
	fmt.Printf("🗑️  卸载 %s\n", name)

	var installPath string
	var err error

	switch packType {
	case types.PackTypeMCP:
		installPath, err = paths.GetMCPPackPath(name)
	case types.PackTypeRule:
		installPath, err = paths.GetRulePackPath(name)
	default:
		return fmt.Errorf("未知的包类型: %s", packType)
	}

	if err != nil {
		return fmt.Errorf("获取安装路径失败: %w", err)
	}

	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		fmt.Printf("  ℹ️  包未安装\n")
		return nil
	}

	if err := os.RemoveAll(installPath); err != nil {
		return fmt.Errorf("删除包失败: %w", err)
	}

	fmt.Printf("✅ %s 卸载完成\n", name)
	return nil
}

// IsInstalled 检查包是否已安装
func (i *MCPInstaller) IsInstalled(name string, packType string) bool {
	var installPath string
	var err error

	switch packType {
	case types.PackTypeMCP:
		installPath, err = paths.GetMCPPackPath(name)
	case types.PackTypeRule:
		installPath, err = paths.GetRulePackPath(name)
	default:
		return false
	}

	if err != nil {
		return false
	}

	_, err = os.Stat(installPath)
	return err == nil
}

// LoadInstalledPack 加载已安装包的 package.json
func (i *MCPInstaller) LoadInstalledPack(name string, packType string) (*types.Pack, error) {
	var installPath string
	var err error

	switch packType {
	case types.PackTypeMCP:
		installPath, err = paths.GetMCPPackPath(name)
	case types.PackTypeRule:
		installPath, err = paths.GetRulePackPath(name)
	default:
		return nil, fmt.Errorf("未知的包类型: %s", packType)
	}

	if err != nil {
		return nil, err
	}

	packageJSONPath := filepath.Join(installPath, "package.json")
	data, err := os.ReadFile(packageJSONPath)
	if err != nil {
		return nil, err
	}

	var pack types.Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		return nil, err
	}

	return &pack, nil
}

// 辅助函数
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr) >= 0)
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func lastIndex(s, substr string) int {
	for i := len(s) - len(substr); i >= 0; i-- {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
