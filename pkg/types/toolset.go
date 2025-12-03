package types

// ToolsetInfo 表示 toolsets.json 中的工具集概要信息
type ToolsetInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	GitHubURL   string `json:"githubUrl"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
}

// Toolset 表示完整的 toolset.json 内容
type Toolset struct {
	Name        string                 `json:"name"`
	DisplayName string                 `json:"displayName,omitempty"`
	Version     string                 `json:"version"`
	Description string                 `json:"description,omitempty"`
	Author      string                 `json:"author,omitempty"`
	License     string                 `json:"license,omitempty"`
	Keywords    []string               `json:"keywords,omitempty"`
	Compatibility map[string]interface{} `json:"compatibility,omitempty"`
	Requirements map[string]interface{}  `json:"requirements,omitempty"`
	Install     InstallConfig          `json:"install"`
	Features    []Feature              `json:"features,omitempty"`
	Scripts     map[string]string      `json:"scripts,omitempty"`
	Documentation map[string]string    `json:"documentation,omitempty"`
	Repository  Repository             `json:"repository,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// InstallConfig 表示安装配置
type InstallConfig struct {
	Targets    map[string]InstallTarget `json:"targets"`
	NpmScripts map[string]string        `json:"npm_scripts,omitempty"`
}

// InstallTarget 表示安装目标配置
type InstallTarget struct {
	Source      string `json:"source"`
	Files       []string `json:"files,omitempty"`
	Merge       bool   `json:"merge,omitempty"`
	Overwrite   bool   `json:"overwrite,omitempty"`
	Executable  bool   `json:"executable,omitempty"`
	Description string `json:"description,omitempty"`
}

// Feature 表示工具集功能
type Feature struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Files       []string `json:"files,omitempty"`
	Essential   bool     `json:"essential,omitempty"`
}

// Repository 表示仓库信息
type Repository struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

