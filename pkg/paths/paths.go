package paths

import (
	"os"
	"path/filepath"
)

const (
	// EnvRootDir 环境变量名，用于指定 Dec 根目录
	// 类似于 pip 的 PYTHONUSERBASE，brew 的 HOMEBREW_PREFIX
	EnvRootDir = "DEC_HOME"
)

// 目录结构设计（重构后）：
// ~/.dec/                        <- DEC_HOME（默认）
// ├── mcp/                       <- MCP 工具包安装目录
// │   └── github-issue/
// │       ├── bin/
// │       ├── rules/
// │       └── dec_package.json
// ├── rules/                     <- 规则包安装目录
// │   └── documentation/
// │       ├── rules/
// │       └── dec_package.json
// ├── registry/                  <- 多注册表目录
// │   ├── local.json             <- 本地开发注册表
// │   ├── test.json              <- 测试注册表
// │   └── registry.json          <- 正式注册表
// ├── cache/                     <- 缓存目录
// │   └── packages/              <- 下载的 tarball 缓存
// ├── config/                    <- 全局配置
// │   ├── system.json            <- 系统配置
// │   └── settings.json          <- 用户设置
// └── bin/                       <- 可执行文件
//     └── dec
//
// 项目配置目录：
// <project>/.dec/config/
// ├── ides.yaml                 <- IDE 配置
// ├── technology.yaml           <- 技术栈
// └── mcp.yaml                  <- MCP 配置

// GetRootDir 获取 Dec 根目录
// 优先级：
// 1. 环境变量 DEC_HOME（如果设置）
// 2. 默认路径：~/.dec
func GetRootDir() (string, error) {
	if rootDir := os.Getenv(EnvRootDir); rootDir != "" {
		return filepath.Abs(rootDir)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".dec"), nil
}

// ========================================
// 全局目录
// ========================================

// GetMCPDir 获取 MCP 工具包安装目录
func GetMCPDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "mcp"), nil
}

// GetRulesDir 获取规则包安装目录
func GetRulesDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "rules"), nil
}

// GetRegistryDir 获取注册表目录
func GetRegistryDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "registry"), nil
}

// GetCacheDir 获取缓存根目录
func GetCacheDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "cache"), nil
}

// GetPackageCacheDir 获取下载包的缓存目录
func GetPackageCacheDir() (string, error) {
	cacheDir, err := GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "packages"), nil
}

// GetConfigDir 获取全局配置文件目录
func GetConfigDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "config"), nil
}

// GetCoreRulesDir 获取核心规则目录
func GetCoreRulesDir() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "core"), nil
}

// GetBinDir 获取可执行文件目录
func GetBinDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "bin"), nil
}

// ========================================
// 项目配置路径
// ========================================

// GetProjectConfigDir 获取项目配置目录
func GetProjectConfigDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".dec", "config")
}

// GetTechnologyConfigPath 获取技术栈配置文件路径
func GetTechnologyConfigPath(projectRoot string) string {
	return filepath.Join(GetProjectConfigDir(projectRoot), "technology.yaml")
}

// GetMCPConfigPath 获取 MCP 配置文件路径
func GetMCPConfigPath(projectRoot string) string {
	return filepath.Join(GetProjectConfigDir(projectRoot), "mcp.yaml")
}

// GetIDEsConfigPath 获取 IDE 配置文件路径
func GetIDEsConfigPath(projectRoot string) string {
	return filepath.Join(GetProjectConfigDir(projectRoot), "ides.yaml")
}

// ========================================
// IDE 输出路径
// ========================================

// IDE 目录名映射
var ideDirectories = map[string]string{
	"cursor":    ".cursor",
	"codebuddy": ".codebuddy",
	"windsurf":  ".windsurf",
	"trae":      ".trae",
}

// GetIDERulesDir 获取指定 IDE 的规则输出目录
func GetIDERulesDir(projectRoot, ide string) string {
	dir, ok := ideDirectories[ide]
	if !ok {
		dir = "." + ide // 默认使用 .{ide} 格式
	}
	return filepath.Join(projectRoot, dir, "rules")
}

// GetIDEMCPConfigPath 获取指定 IDE 的 MCP 配置文件路径
func GetIDEMCPConfigPath(projectRoot, ide string) string {
	dir, ok := ideDirectories[ide]
	if !ok {
		dir = "." + ide
	}
	return filepath.Join(projectRoot, dir, "mcp.json")
}

// GetCursorRulesDir 获取 Cursor 规则输出目录
func GetCursorRulesDir(projectRoot string) string {
	return GetIDERulesDir(projectRoot, "cursor")
}

// GetCursorMCPConfigPath 获取 Cursor MCP 配置文件路径
func GetCursorMCPConfigPath(projectRoot string) string {
	return GetIDEMCPConfigPath(projectRoot, "cursor")
}

// ========================================
// 技术栈配置路径
// ========================================

// GetTechStackDir 获取技术栈配置目录
// 返回 Dec 安装目录下的 config/techstack 目录
func GetTechStackDir() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "techstack"), nil
}

// ========================================
// 工具函数
// ========================================

// EnsureDir 确保目录存在
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// EnsureAllDirs 确保所有必要的目录存在
func EnsureAllDirs() error {
	dirs := []func() (string, error){
		GetMCPDir,
		GetRulesDir,
		GetRegistryDir,
		GetPackageCacheDir,
		GetConfigDir,
		GetBinDir,
	}

	for _, getDirFunc := range dirs {
		dir, err := getDirFunc()
		if err != nil {
			return err
		}
		if err := EnsureDir(dir); err != nil {
			return err
		}
	}

	return nil
}


