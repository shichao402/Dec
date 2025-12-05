package paths

import (
	"os"
	"path/filepath"
)

const (
	// EnvRootDir 环境变量名，用于指定 CursorToolset 根目录
	// 类似于 pip 的 PYTHONUSERBASE，brew 的 HOMEBREW_PREFIX
	EnvRootDir = "CURSOR_TOOLSET_HOME"

	// RegistryURL 默认的 registry 下载地址
	RegistryURL = "https://github.com/firoyang/CursorToolset/releases/download/registry/registry.json"
)

// 目录结构设计（参考 pip/brew）：
// ~/.cursortoolsets/              <- CURSOR_TOOLSET_HOME（默认）
// ├── repos/                      <- 已安装的包（解压后的内容）
// │   ├── github-action-toolset/
// │   └── other-toolset/
// ├── cache/                      <- 缓存目录
// │   ├── packages/               <- 下载的 tarball 缓存
// │   │   └── github-action-toolset-1.0.0.tar.gz
// │   └── manifests/              <- manifest 缓存
// │       └── github-action-toolset.json
// ├── config/                     <- 配置文件
// │   └── registry.json           <- 本地 registry 缓存
// └── bin/                        <- CursorToolset 自身的可执行文件
//     └── cursortoolset

// GetRootDir 获取 CursorToolset 根目录
// 优先级：
// 1. 环境变量 CURSOR_TOOLSET_HOME（如果设置）
// 2. 默认路径：~/.cursortoolsets
func GetRootDir() (string, error) {
	if rootDir := os.Getenv(EnvRootDir); rootDir != "" {
		return filepath.Abs(rootDir)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".cursortoolsets"), nil
}

// GetReposDir 获取已安装包的目录
func GetReposDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "repos"), nil
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

// GetManifestCacheDir 获取 manifest 缓存目录
func GetManifestCacheDir() (string, error) {
	cacheDir, err := GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "manifests"), nil
}

// GetConfigDir 获取配置文件目录
func GetConfigDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "config"), nil
}

// GetBinDir 获取可执行文件目录
func GetBinDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "bin"), nil
}

// GetRegistryPath 获取本地 registry 缓存文件路径
func GetRegistryPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "registry.json"), nil
}

// GetManifestPath 获取指定包的 manifest 缓存路径
func GetManifestPath(packageName string) (string, error) {
	manifestDir, err := GetManifestCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(manifestDir, packageName+".json"), nil
}

// GetPackagePath 获取已安装包的路径
func GetPackagePath(packageName string) (string, error) {
	reposDir, err := GetReposDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(reposDir, packageName), nil
}

// GetPackageCachePath 获取下载包的缓存路径
func GetPackageCachePath(packageName, version string) (string, error) {
	cacheDir, err := GetPackageCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, packageName+"-"+version+".tar.gz"), nil
}

// EnsureDir 确保目录存在
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// EnsureAllDirs 确保所有必要的目录存在
func EnsureAllDirs() error {
	dirs := []func() (string, error){
		GetReposDir,
		GetPackageCacheDir,
		GetManifestCacheDir,
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

// ========================================
// 兼容旧版本的函数（逐步废弃）
// ========================================

// GetToolsetsDir 获取工具集安装目录（为了向后兼容保留）
// Deprecated: 使用 GetReposDir 替代
func GetToolsetsDir(workDir string) (string, error) {
	return GetReposDir()
}

// GetToolsetConfigPath 获取 available-toolsets.json 配置文件路径
// Deprecated: 使用 GetRegistryPath 替代
func GetToolsetConfigPath() (string, error) {
	return GetRegistryPath()
}
