package paths

import (
	"os"
	"path/filepath"
)

const (
	// EnvRootDir 环境变量名，用于指定 CursorToolset 根目录
	// 类似于 pip 的 PYTHONUSERBASE，brew 的 HOMEBREW_PREFIX
	EnvRootDir = "CURSOR_TOOLSET_HOME"
)

// 目录结构设计（参考 pip/brew）：
// ~/.cursortoolsets/              <- CURSOR_TOOLSET_HOME（默认）
// ├── repos/                      <- 工具集仓库源码（类似 brew 的 Cellar）
// │   ├── github-action-toolset/
// │   └── other-toolset/
// ├── config/                     <- 配置文件
// │   └── available-toolsets.json
// └── bin/                        <- CursorToolset 自身的可执行文件（可选）
//     └── cursortoolset

// GetRootDir 获取 CursorToolset 根目录
// 这是所有 CursorToolset 相关文件的根目录（类似 ~/.local 或 /usr/local）
// 优先级：
// 1. 环境变量 CURSOR_TOOLSET_HOME（如果设置）
// 2. 默认路径：~/.cursortoolsets
func GetRootDir() (string, error) {
	// 优先使用环境变量
	if rootDir := os.Getenv(EnvRootDir); rootDir != "" {
		return filepath.Abs(rootDir)
	}

	// 使用默认路径（用户主目录下，独立于 .cursor 系统目录）
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".cursortoolsets"), nil
}

// GetReposDir 获取工具集仓库目录
// 这个目录存储所有工具集的 Git 仓库（类似 brew 的 Cellar）
func GetReposDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "repos"), nil
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
// 返回 CursorToolset 自身的 bin 目录路径
func GetBinDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "bin"), nil
}

// GetToolsetsDir 获取工具集安装目录（为了向后兼容保留）
// 现在实际上返回 repos 目录
func GetToolsetsDir(workDir string) (string, error) {
	return GetReposDir()
}

// GetToolsetConfigPath 获取 available-toolsets.json 配置文件路径
func GetToolsetConfigPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "available-toolsets.json"), nil
}

