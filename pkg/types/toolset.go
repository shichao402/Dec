package types

import "strings"

// ========================================
// Registry 相关类型（包索引）
// ========================================

// Registry 表示包注册表（发布在 GitHub Release 中）
type Registry struct {
	Version   string         `json:"version"`              // Registry 格式版本
	UpdatedAt string         `json:"updated_at,omitempty"` // 最后更新时间
	Packages  []RegistryItem `json:"packages"`             // 包列表
}

// RegistryItem 表示注册表中的包条目
// 包含完整的包元信息，由 CI 自动同步
type RegistryItem struct {
	Repository  string       `json:"repository"`            // 仓库地址（如 https://github.com/user/repo）
	Name        string       `json:"name,omitempty"`        // 包名
	Version     string       `json:"version,omitempty"`     // 当前版本
	Description string       `json:"description,omitempty"` // 描述
	Author      string       `json:"author,omitempty"`      // 作者
	Dist        Distribution `json:"dist,omitempty"`        // 分发信息
}

// GetRepoName 从 repository URL 提取仓库名作为标识符
func (r RegistryItem) GetRepoName() string {
	// https://github.com/user/repo -> repo
	// https://github.com/user/repo.git -> repo
	repo := r.Repository
	repo = strings.TrimSuffix(repo, "/")
	repo = strings.TrimSuffix(repo, ".git")
	parts := strings.Split(repo, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// ========================================
// Package Manifest 相关类型（包自描述）
// ========================================

// Manifest 表示包的自描述文件（package.json）
// 由包开发者维护，包含包的完整元信息
type Manifest struct {
	// 基本信息
	Name        string   `json:"name"`                  // 包名（必须与 registry 中一致）
	DisplayName string   `json:"displayName,omitempty"` // 显示名称
	Version     string   `json:"version"`               // 当前版本（语义化版本）
	Description string   `json:"description,omitempty"` // 描述
	Author      string   `json:"author,omitempty"`      // 作者
	License     string   `json:"license,omitempty"`     // 许可证
	Keywords    []string `json:"keywords,omitempty"`    // 关键词（用于搜索）

	// 仓库信息
	Repository Repository `json:"repository,omitempty"` // Git 仓库信息

	// 分发信息
	Dist Distribution `json:"dist"` // 下载和校验信息

	// 可执行程序配置（可选）
	// 支持两种格式：
	// 1. 简单格式: {"cmd": "path/to/binary"}
	// 2. 多平台格式: {"cmd": {"darwin-arm64": "path/to/binary-darwin-arm64", ...}}
	Bin map[string]interface{} `json:"bin,omitempty"` // 需要暴露的可执行程序

	// 构建配置（可选）
	Build *BuildConfig `json:"build,omitempty"` // 构建配置

	// 发布配置（可选）
	Release *ReleaseConfig `json:"release,omitempty"` // 发布配置

	// 管理器兼容性
	CursorToolset ManagerCompat `json:"cursortoolset,omitempty"` // 管理器兼容性要求

	// 依赖（可选）
	Dependencies []string `json:"dependencies,omitempty"` // 依赖的包名列表
}

// BuildConfig 表示构建配置
type BuildConfig struct {
	Type      string   `json:"type,omitempty"`      // 构建类型: go, rust, node 等
	Entry     string   `json:"entry,omitempty"`     // 入口文件
	Output    string   `json:"output,omitempty"`    // 输出路径
	Platforms []string `json:"platforms,omitempty"` // 目标平台列表
}

// ReleaseConfig 表示发布配置
type ReleaseConfig struct {
	Exclude []string `json:"exclude,omitempty"` // 打包时排除的文件
	GitHub  bool     `json:"github,omitempty"`  // 是否发布到 GitHub Release
}

// Distribution 表示包的分发信息
type Distribution struct {
	Tarball string `json:"tarball"`        // 下载文件名（相对路径）或完整 URL（向后兼容）
	SHA256  string `json:"sha256"`         // SHA256 校验和
	Size    int64  `json:"size,omitempty"` // 文件大小（字节）
}

// Repository 表示仓库信息
type Repository struct {
	Type string `json:"type,omitempty"` // 仓库类型（git）
	URL  string `json:"url"`            // 仓库地址
}

// ManagerCompat 表示管理器兼容性要求
type ManagerCompat struct {
	MinVersion string `json:"minVersion,omitempty"` // 最低管理器版本
}

// ========================================
// 本地状态相关类型
// ========================================

// InstalledPackage 表示已安装的包信息（本地状态）
type InstalledPackage struct {
	Name        string `json:"name"`        // 包名
	Version     string `json:"version"`     // 已安装版本
	InstallTime string `json:"installTime"` // 安装时间
	Path        string `json:"path"`        // 安装路径
}

// CachedManifest 表示缓存的包 manifest
type CachedManifest struct {
	Manifest
	CachedAt string `json:"cachedAt"` // 缓存时间
}

