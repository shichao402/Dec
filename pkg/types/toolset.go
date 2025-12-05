package types

// ========================================
// Registry 相关类型（包索引）
// ========================================

// Registry 表示包注册表（发布在 GitHub Release 中）
type Registry struct {
	Version  string         `json:"version"`  // Registry 格式版本
	Packages []RegistryItem `json:"packages"` // 包列表
}

// RegistryItem 表示注册表中的包条目（最小信息）
type RegistryItem struct {
	Name        string `json:"name"`        // 包名（唯一标识）
	ManifestURL string `json:"manifestUrl"` // 包自描述文件地址
}

// ========================================
// Package Manifest 相关类型（包自描述）
// ========================================

// Manifest 表示包的自描述文件（toolset.json）
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

	// 管理器兼容性
	CursorToolset ManagerCompat `json:"cursortoolset,omitempty"` // 管理器兼容性要求

	// 依赖（可选）
	Dependencies []string `json:"dependencies,omitempty"` // 依赖的包名列表
}

// Distribution 表示包的分发信息
type Distribution struct {
	Tarball string `json:"tarball"`         // 下载地址（tar.gz）
	SHA256  string `json:"sha256"`          // SHA256 校验和
	Size    int64  `json:"size,omitempty"`  // 文件大小（字节）
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

// ========================================
// 兼容旧版本的类型（逐步废弃）
// ========================================

// ToolsetInfo 表示 available-toolsets.json 中的工具集概要信息
// Deprecated: 使用 RegistryItem + Manifest 替代
type ToolsetInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	GitHubURL   string `json:"githubUrl,omitempty"`   // 旧字段，兼容
	ManifestURL string `json:"manifestUrl,omitempty"` // 新字段
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
}

// Toolset 表示完整的 toolset.json 内容
// Deprecated: 使用 Manifest 替代
type Toolset struct {
	Name          string                 `json:"name"`
	DisplayName   string                 `json:"displayName,omitempty"`
	Version       string                 `json:"version"`
	Description   string                 `json:"description,omitempty"`
	Author        string                 `json:"author,omitempty"`
	License       string                 `json:"license,omitempty"`
	Keywords      []string               `json:"keywords,omitempty"`
	Compatibility map[string]interface{} `json:"compatibility,omitempty"`
	Requirements  map[string]interface{} `json:"requirements,omitempty"`
	Install       InstallConfig          `json:"install"`
	Features      []Feature              `json:"features,omitempty"`
	Scripts       map[string]string      `json:"scripts,omitempty"`
	Documentation map[string]string      `json:"documentation,omitempty"`
	Repository    Repository             `json:"repository,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// InstallConfig 表示安装配置
// Deprecated: 新版本不再需要复杂的安装配置
type InstallConfig struct {
	Targets    map[string]InstallTarget `json:"targets"`
	NpmScripts map[string]string        `json:"npm_scripts,omitempty"`
}

// InstallTarget 表示安装目标配置
// Deprecated: 新版本直接解压，不再需要
type InstallTarget struct {
	Source      string   `json:"source"`
	Files       []string `json:"files,omitempty"`
	Merge       bool     `json:"merge,omitempty"`
	Overwrite   bool     `json:"overwrite,omitempty"`
	Executable  bool     `json:"executable,omitempty"`
	Description string   `json:"description,omitempty"`
}

// Feature 表示工具集功能
// Deprecated: 新版本简化，不再需要
type Feature struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Files       []string `json:"files,omitempty"`
	Essential   bool     `json:"essential,omitempty"`
}
