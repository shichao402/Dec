package version

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// VersionInfo 版本信息结构
type VersionInfo struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
	Commit    string `json:"commit"`
	Branch    string `json:"branch"`
}

// LoadVersionInfo 从 version.json 加载版本信息
func LoadVersionInfo(workDir string) (*VersionInfo, error) {
	// 查找 version.json 文件
	versionPath := findVersionFile(workDir)
	if versionPath == "" {
		return nil, fmt.Errorf("未找到 version.json 文件")
	}

	data, err := os.ReadFile(versionPath)
	if err != nil {
		return nil, fmt.Errorf("读取 version.json 失败: %w", err)
	}

	var info VersionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("解析 version.json 失败: %w", err)
	}

	// 验证版本号格式
	if info.Version == "" {
		return nil, fmt.Errorf("version.json 中 version 字段为空")
	}

	return &info, nil
}

// findVersionFile 查找 version.json 文件
// 从当前目录开始向上查找，直到找到或到达项目根目录
func findVersionFile(startDir string) string {
	dir := startDir
	maxDepth := 10 // 防止无限循环

	for i := 0; i < maxDepth; i++ {
		versionPath := filepath.Join(dir, "version.json")
		if _, err := os.Stat(versionPath); err == nil {
			return versionPath
		}

		// 检查是否到达根目录
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return ""
}

// GetVersion 获取版本号（简化接口）
func GetVersion(workDir string) (string, error) {
	info, err := LoadVersionInfo(workDir)
	if err != nil {
		return "", err
	}
	return info.Version, nil
}

// UpdateVersionInfo 更新 version.json 中的字段
func UpdateVersionInfo(workDir string, updates map[string]string) error {
	versionPath := findVersionFile(workDir)
	if versionPath == "" {
		return fmt.Errorf("未找到 version.json 文件")
	}

	info, err := LoadVersionInfo(workDir)
	if err != nil {
		return err
	}

	// 更新字段
	if buildTime, ok := updates["build_time"]; ok {
		info.BuildTime = buildTime
	}
	if commit, ok := updates["commit"]; ok {
		info.Commit = commit
	}
	if branch, ok := updates["branch"]; ok {
		info.Branch = branch
	}
	if version, ok := updates["version"]; ok {
		info.Version = version
	}

	// 写回文件
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化版本信息失败: %w", err)
	}

	if err := os.WriteFile(versionPath, data, 0644); err != nil {
		return fmt.Errorf("写入 version.json 失败: %w", err)
	}

	return nil
}

