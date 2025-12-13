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

// 目录结构设计（参考 pip/brew）：
// ~/.decs/              <- DEC_HOME（默认）
// ├── repos/                      <- 已安装的包（解压后的内容）
// │   ├── github-action-toolset/
// │   │   └── package.json        <- 包的自描述文件
// │   └── other-toolset/
// ├── cache/                      <- 缓存目录
// │   ├── packages/               <- 下载的 tarball 缓存
// │   │   └── github-action-toolset-1.0.0.tar.gz
// │   └── manifests/              <- manifest 缓存
// │       └── github-action-toolset.jsondec update --self

// ├── config/                     <- 配置文件
// │   └── registry.json           <- 本地 registry 缓存
// ├── bin/                        <- 可执行文件（包暴露的命令 + dec）
// │   ├── dec
// │   └── gh-action-debug -> ../repos/github-action-toolset/...
// └── docs/                       <- 包开发文档（供 init 命令复制）
//     └── package-dev-guide.md

// GetRootDir 获取 Dec 根目录u
// 优先级：
// 1. 环境变量 DEC_HOME（如果设置）
// 2. 默认路径：~/.decs
func GetRootDir() (string, error) {
	if rootDir := os.Getenv(EnvRootDir); rootDir != "" {
		return filepath.Abs(rootDir)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".decs"), nil
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
