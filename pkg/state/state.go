// Package state 管理 Dec 的运行时状态
//
// 状态文件存储在 ~/.decs/.state/ 目录下：
//   - version: 当前安装的二进制版本（由 install.sh 或 update --self 写入）
//   - docs_version: 文档的版本（由 setup 逻辑写入）
//
// 设计原则：
//   - 单一职责：每个组件只写自己负责的状态
//   - 事件驱动：状态变化后，由独立的响应者检测并处理
package state

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/paths"
)

const (
	// StateDir 状态目录名
	StateDir = ".state"

	// VersionFile 二进制版本状态文件
	VersionFile = "version"

	// DocsVersionFile 文档版本状态文件
	DocsVersionFile = "docs_version"
)

// GetStateDir 获取状态目录路径
func GetStateDir() (string, error) {
	rootDir, err := paths.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, StateDir), nil
}

// EnsureStateDir 确保状态目录存在
func EnsureStateDir() error {
	stateDir, err := GetStateDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(stateDir, 0755)
}

// GetVersion 读取当前安装的二进制版本
func GetVersion() (string, error) {
	return readStateFile(VersionFile)
}

// SetVersion 写入当前安装的二进制版本
func SetVersion(version string) error {
	return writeStateFile(VersionFile, version)
}

// GetDocsVersion 读取文档版本
func GetDocsVersion() (string, error) {
	return readStateFile(DocsVersionFile)
}

// SetDocsVersion 写入文档版本
func SetDocsVersion(version string) error {
	return writeStateFile(DocsVersionFile, version)
}

// NeedDocsUpdate 检查是否需要更新文档
// 当二进制版本与文档版本不一致时返回 true
func NeedDocsUpdate() (bool, string) {
	version, err := GetVersion()
	if err != nil || version == "" {
		// 状态文件不存在，可能是旧版本安装，不强制更新
		return false, ""
	}

	docsVersion, _ := GetDocsVersion()
	return version != docsVersion, version
}

// readStateFile 读取状态文件
func readStateFile(filename string) (string, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(filepath.Join(stateDir, filename))
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

// writeStateFile 写入状态文件
func writeStateFile(filename, content string) error {
	if err := EnsureStateDir(); err != nil {
		return err
	}

	stateDir, err := GetStateDir()
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(stateDir, filename), []byte(content), 0644)
}
