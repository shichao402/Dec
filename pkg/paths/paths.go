package paths

import (
	"os"
	"path/filepath"
)

const (
	// EnvRootDir 环境变量名，用于指定 Dec 根目录
	EnvRootDir = "DEC_HOME"
)

// 目录结构：
// ~/.dec/                        <- DEC_HOME（默认）
// ├── config.yaml                <- 全局配置（vault_source）
// └── vault/                     <- 个人知识仓库（Git 管理）
//
// 项目配置目录：
// <project>/.dec/config/
// ├── ides.yaml                 <- IDE 配置
// └── vault.yaml                <- Vault 资产声明

// GetRootDir 获取 Dec 根目录
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

// GetVaultDir 获取个人知识仓库目录
func GetVaultDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "vault"), nil
}

// ========================================
// 项目配置路径
// ========================================

// GetProjectConfigDir 获取项目配置目录
func GetProjectConfigDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".dec", "config")
}

// GetIDEsConfigPath 获取 IDE 配置文件路径
func GetIDEsConfigPath(projectRoot string) string {
	return filepath.Join(GetProjectConfigDir(projectRoot), "ides.yaml")
}

// GetVaultConfigPath 获取 Vault 配置文件路径
func GetVaultConfigPath(projectRoot string) string {
	return filepath.Join(GetProjectConfigDir(projectRoot), "vault.yaml")
}

// ========================================
// 工具函数
// ========================================

// EnsureDir 确保目录存在
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}
