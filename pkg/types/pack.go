package types

// ========================================
// 包类型常量
// ========================================

const (
	PackTypeRule = "rule" // 规则包
	PackTypeMCP  = "mcp"  // MCP 工具包
)

// 包分类常量
const (
	PackCategoryCore      = "core"      // 核心规则（始终启用）
	PackCategoryLanguage  = "language"  // 语言规则
	PackCategoryFramework = "framework" // 框架规则
	PackCategoryFeature   = "feature"   // 功能规则
	PackCategoryTool      = "tool"      // 工具包
)

// ========================================
// 统一包元数据类型（package.json）
// ========================================

// Pack 表示包的元数据定义（规则包或 MCP 工具包）
type Pack struct {
	// 基本信息
	Name        string `json:"name"`                  // 包名（唯一标识）
	DisplayName string `json:"displayName,omitempty"` // 显示名称
	Description string `json:"description,omitempty"` // 描述
	Version     string `json:"version,omitempty"`     // 版本号
	Author      string `json:"author,omitempty"`      // 作者
	License     string `json:"license,omitempty"`     // 许可证

	// 包类型
	Type     string `json:"type"`               // 包类型: rule, mcp
	Category string `json:"category,omitempty"` // 分类: core, language, framework, feature, tool

	// 仓库信息
	Repository Repository `json:"repository,omitempty"` // Git 仓库

	// 规则包特有字段
	Rules []string `json:"rules,omitempty"` // 规则文件列表（相对路径）

	// MCP 工具包特有字段
	MCP *MCPPackConfig `json:"mcp,omitempty"` // MCP Server 配置

	// 附带规则（MCP 工具包可选）
	AttachedRules []AttachedRule `json:"attached_rules,omitempty"` // 附带的工作流程规则

	// 依赖
	Requires    []string         `json:"requires,omitempty"`    // 依赖的其他规则包
	Environment []EnvDependency  `json:"environment,omitempty"` // 依赖的环境（flutter, node 等）

	// 配置模式
	ConfigSchema map[string]ConfigField `json:"config_schema,omitempty"` // 可配置项定义

	// 分发信息（由 CI 填充）
	Dist *Distribution `json:"dist,omitempty"` // 下载和校验信息
}

// MCPPackConfig 表示 MCP 工具包的 Server 配置
type MCPPackConfig struct {
	Command string            `json:"command"`           // 启动命令（相对于包目录）
	Args    []string          `json:"args,omitempty"`    // 命令参数
	Env     map[string]string `json:"env,omitempty"`     // 环境变量模板
}

// AttachedRule 表示 MCP 工具包附带的规则
type AttachedRule struct {
	File        string `json:"file"`                  // 规则文件路径（相对于包目录）
	Description string `json:"description,omitempty"` // 规则描述
}

// Distribution 表示包的分发信息
type Distribution struct {
	Tarball string `json:"tarball"`        // 下载文件名或 URL
	SHA256  string `json:"sha256"`         // SHA256 校验和
	Size    int64  `json:"size,omitempty"` // 文件大小（字节）
}

// EnvDependency 表示环境依赖
type EnvDependency struct {
	Name         string `json:"name"`                    // 环境名称（flutter, node, python 等）
	Check        string `json:"check,omitempty"`         // 检查命令
	InstallGuide string `json:"install_guide,omitempty"` // 安装指南 URL
}

// ConfigField 表示配置字段定义
type ConfigField struct {
	Type        string      `json:"type"`                  // 类型: string, bool, number
	Description string      `json:"description,omitempty"` // 描述
	Default     interface{} `json:"default,omitempty"`     // 默认值
	Required    bool        `json:"required,omitempty"`    // 是否必填
}

// Repository 表示仓库信息
type Repository struct {
	Type string `json:"type,omitempty"` // 仓库类型（git）
	URL  string `json:"url"`            // 仓库地址
}

// ========================================
// 项目配置类型（.dec/config/）
// ========================================

// ProjectConfig 表示项目配置（project.json）
type ProjectConfig struct {
	Name        string   `json:"name"`                  // 项目名称
	Description string   `json:"description,omitempty"` // 项目描述
	IDEs        []string `json:"ides,omitempty"`        // 目标 IDE: cursor, codebuddy, windsurf, trae
}

// TechnologyConfig 表示技术栈配置（technology.json）
type TechnologyConfig struct {
	Languages  []string `json:"languages,omitempty"`  // 语言: go, dart, typescript, python 等
	Frameworks []string `json:"frameworks,omitempty"` // 框架: flutter, react, vue, django 等
	Platforms  []string `json:"platforms,omitempty"`  // 平台: web, android, ios, macos, windows, linux
}

// PacksConfig 表示包配置（packs.json）
type PacksConfig map[string]PackEntry

// PackEntry 表示单个包的配置
type PackEntry struct {
	Enabled bool                   `json:"enabled"`          // 是否启用
	Type    string                 `json:"type,omitempty"`   // 类型: rule, mcp
	Config  map[string]interface{} `json:"config,omitempty"` // 用户配置
}

// ========================================
// MCP 配置类型（.cursor/mcp.json）
// ========================================

// MCPConfig 表示 MCP 配置文件
type MCPConfig struct {
	MCPServers map[string]MCPServer `json:"mcpServers"`
}

// MCPServer 表示单个 MCP Server 配置
type MCPServer struct {
	Command string            `json:"command"`        // 启动命令
	Args    []string          `json:"args,omitempty"` // 命令参数
	Env     map[string]string `json:"env,omitempty"`  // 环境变量
}

// ========================================
// 多注册表类型
// ========================================

// RegistryType 注册表类型
type RegistryType string

const (
	RegistryTypeLocal    RegistryType = "local"    // 本地开发注册表
	RegistryTypeTest     RegistryType = "test"     // 测试注册表
	RegistryTypeOfficial RegistryType = "official" // 正式注册表
)

// PackRegistry 表示包注册表
type PackRegistry struct {
	Version   string                  `json:"version"`              // 注册表版本
	UpdatedAt string                  `json:"updated_at,omitempty"` // 更新时间
	Packs     map[string]PackMetadata `json:"packs"`                // 包列表（key 为包名）
}

// PackMetadata 表示注册表中的包元数据
type PackMetadata struct {
	Name        string `json:"name"`                  // 包名
	Description string `json:"description,omitempty"` // 描述
	Type        string `json:"type"`                  // 类型: rule, mcp
	Category    string `json:"category,omitempty"`    // 分类
	Builtin     bool   `json:"builtin,omitempty"`     // 是否内置
	Repository  string `json:"repository,omitempty"`  // 仓库地址（外部包）
	Version     string `json:"version,omitempty"`     // 版本

	// 本地开发包特有字段
	LocalPath string `json:"local_path,omitempty"` // 本地路径（仅 local registry）
	LinkedAt  string `json:"linked_at,omitempty"`  // 链接时间（仅 local registry）
}

// ResolvedPack 表示解析后的包信息（包含来源）
type ResolvedPack struct {
	PackMetadata
	Source       RegistryType `json:"source"`                  // 来源注册表
	InstallPath  string       `json:"install_path,omitempty"`  // 安装路径
	IsInstalled  bool         `json:"is_installed"`            // 是否已安装
}
