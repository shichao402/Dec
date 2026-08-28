package types

import (
	"path/filepath"
	"regexp"
	"strings"
)

// IDEsConfig 表示 IDE 配置
type IDEsConfig struct {
	IDEs []string `yaml:"ides,omitempty" json:"ides,omitempty"`
}

// MCPConfig 表示 IDE 的 MCP 配置。
//
// 对于大多数 IDE，对应的是 JSON 文件（例如 .cursor/mcp.json）；
// 对于 Codex，对应的是 config.toml 中的 [mcp_servers] 段。
type MCPConfig struct {
	MCPServers map[string]MCPServer `json:"mcpServers"`
	Extra      map[string]any       `json:"-"`
}

// MCPServer 表示单个 MCP Server 配置
type MCPServer struct {
	Command           string            `json:"command,omitempty"`
	Args              []string          `json:"args,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	EnvVars           []string          `json:"env_vars,omitempty"`
	Cwd               string            `json:"cwd,omitempty"`
	URL               string            `json:"url,omitempty"`
	BearerTokenEnvVar string            `json:"bearer_token_env_var,omitempty"`
	HTTPHeaders       map[string]string `json:"http_headers,omitempty"`
	EnvHTTPHeaders    map[string]string `json:"env_http_headers,omitempty"`
	StartupTimeoutSec *int              `json:"startup_timeout_sec,omitempty"`
	ToolTimeoutSec    *int              `json:"tool_timeout_sec,omitempty"`
	Enabled           *bool             `json:"enabled,omitempty"`
	Required          *bool             `json:"required,omitempty"`
	EnabledTools      []string          `json:"enabled_tools,omitempty"`
	DisabledTools     []string          `json:"disabled_tools,omitempty"`
	Scopes            []string          `json:"scopes,omitempty"`
	Extra             map[string]any    `json:"-"`
}

// ConfigKind 区分本机全局配置与工作区项目配置，避免互相误升级。
type ConfigKind string

const (
	ConfigKindGlobal  ConfigKind = "global"
	ConfigKindProject ConfigKind = "project"
)

const (
	// GlobalConfigVersion 是 ~/.dec/config.yaml 字段 schema。
	GlobalConfigVersion = 1
	// ProjectConfigSchemaVersion 是工作区 .dec/config.yaml 字段 schema（沿用 v2 字符串）。
	ProjectConfigSchemaVersion = 2
	// LocalLayoutVersion 是 cache / secrets 派生目录布局。
	LocalLayoutVersion = 1
)

// GlobalConfig 全局配置结构 (~/.dec/config.yaml)
type GlobalConfig struct {
	Kind              ConfigKind `yaml:"kind,omitempty"`
	Version           int        `yaml:"version,omitempty"`
	LayoutVersion     int        `yaml:"layout_version,omitempty"`
	RepoURL           string     `yaml:"repo_url,omitempty"`
	IDEs              []string   `yaml:"ides,omitempty"`
	Editor            string     `yaml:"editor,omitempty"`
	ServerIdleTimeout string     `yaml:"server_idle_timeout,omitempty"`
	// EnabledProjects 是本机启用的 Project 列表。
	EnabledProjects []string `yaml:"enabled_projects,omitempty"`
	// EnabledBundles 仅用于读取旧配置；运行时会归一到当前启用列表。
	EnabledBundles []string `yaml:"enabled_bundles,omitempty"`
}

const ProjectConfigVersionV2 = "v2"

// ProjectManifestFileName 是顶层 Project 的声明文件。
const ProjectManifestFileName = "dec.yaml"

// ProjectNamePattern 是 Project 名的唯一命名契约：小写 kebab-case。
const ProjectNamePattern = `^[a-z0-9]+(?:-[a-z0-9]+)*$`

// PNamePattern 是 ProjectNamePattern 的旧名。
const PNamePattern = ProjectNamePattern

var projectNameRegexp = regexp.MustCompile(ProjectNamePattern)

// AssetVisibility 表示资产能否被其他项目引用。
type AssetVisibility string

const (
	AssetVisibilityPublic  AssetVisibility = "public"
	AssetVisibilityPrivate AssetVisibility = "private"
)

// AssetPlane 表示资产安装到本机还是本仓库。
type AssetPlane string

const (
	AssetPlaneGlobal AssetPlane = "global"
	AssetPlaneLocal  AssetPlane = "local"
	// 旧象限目录名，仅扫描/迁移识别。
	AssetPlaneUser    AssetPlane = "user"
	AssetPlaneProject AssetPlane = "project"
)

// CanonicalAssetPlane 把旧象限名归一到 global/local。
func CanonicalAssetPlane(plane AssetPlane) AssetPlane {
	switch plane {
	case AssetPlaneUser, AssetPlaneGlobal:
		return AssetPlaneGlobal
	case AssetPlaneProject, AssetPlaneLocal, "":
		return AssetPlaneLocal
	default:
		return plane
	}
}

// Project 是 Git 仓库顶层唯一可写单元，对应 <name>/dec.yaml。
type Project struct {
	Name        string   `yaml:"name"`
	Title       string   `yaml:"title,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Requires    []string `yaml:"requires,omitempty"`
	IDEs        []string `yaml:"ides,omitempty"`
	Editor      string   `yaml:"editor,omitempty"`
}

// P 是 Project 的旧类型名。
type P = Project

func IsValidProjectName(name string) bool {
	return projectNameRegexp.MatchString(strings.TrimSpace(name))
}

func IsValidPName(name string) bool {
	return IsValidProjectName(name)
}

func ProjectManifestPath(name string) string {
	return filepath.Join(name, ProjectManifestFileName)
}

func PManifestPath(name string) string {
	return ProjectManifestPath(name)
}

func ProjectQuadrantDir(name string, visibility AssetVisibility, plane AssetPlane) string {
	return filepath.Join(name, string(visibility), string(CanonicalAssetPlane(plane)))
}

func PQuadrantDir(name string, visibility AssetVisibility, plane AssetPlane) string {
	return ProjectQuadrantDir(name, visibility, plane)
}

// VaultProjectsDir 是 Git Vault 中 project 声明目录。
const VaultProjectsDir = "projects"

// VaultBundlesDir 是 Git Vault 中 bundle 根目录。
const VaultBundlesDir = "bundles"

// BundleManifestFileName 是每个 bundle 目录内的声明文件名。
const BundleManifestFileName = "bundle.yaml"

// VaultProjectFileExt 是 project 声明文件扩展名。
const VaultProjectFileExt = ".yaml"

// LegacyVaultProject 描述旧模型 vault 中 projects/<name>.yaml。
type LegacyVaultProject struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Bundles     []string `yaml:"bundles"`
	IDEs        []string `yaml:"ides,omitempty"`
	Editor      string   `yaml:"editor,omitempty"`
}

// VaultProjectPath 返回 vault 内 project 声明的相对路径。
func VaultProjectPath(name string) string {
	return VaultProjectsDir + "/" + name + VaultProjectFileExt
}

// VaultBundleDir 返回 vault 内 bundle 目录的相对路径。
func VaultBundleDir(name string) string {
	return VaultBundlesDir + "/" + name
}

// VaultBundleManifestPath 返回 bundle 声明文件的相对路径。
func VaultBundleManifestPath(name string) string {
	return VaultBundleDir(name) + "/" + BundleManifestFileName
}

// ProjectConfig 工作区配置 (<workspace>/.dec/config.yaml)
type ProjectConfig struct {
	Kind          ConfigKind `yaml:"kind,omitempty"`
	Version       string     `yaml:"version,omitempty"`
	LayoutVersion int        `yaml:"layout_version,omitempty"`
	// ProjectName 是绑定项目名，对应 vault <name>/dec.yaml。
	ProjectName string   `yaml:"project_name,omitempty"`
	IDEs        []string `yaml:"ides,omitempty"`
	Editor      string   `yaml:"editor,omitempty"`
	// EnabledBundles 是旧启用列表；P 模型下 requires 才是 SSOT。
	EnabledBundles []string `yaml:"enabled_bundles,omitempty"`
}

// BundleScope 是 bundle 的二元作用域（ADR 0009）。
type BundleScope string

const (
	BundleScopeUser    BundleScope = "user"
	BundleScopeProject BundleScope = "project"
)

// Bundle 描述 vault 内声明的一组资产启用单位。
//
// Bundle 的 YAML 声明位于 bundles/<name>/bundle.yaml：
//
//	name: vikunja
//	scope: project
//	description: Vikunja 任务管理完整工作流
//	members:
//	  - mcp/vikunja-mcp
//	  - rules/vikunja-integration
//	  - skills/vikunja-workflow
//
// 成员资产须位于同一 bundles/<name>/ 目录内；成员只能是 skill/command/rule/mcp（不能是 bundle）。
type Bundle struct {
	// Name 为 bundle 短名，在 vault 内唯一，用于 config.yaml 引用。
	Name string `yaml:"name"`
	// Scope 为 user | project（ADR 0009）；决定启用平面与落地平面。
	Scope BundleScope `yaml:"scope"`
	// Description 是 TUI 渲染用的一句话描述。
	Description string `yaml:"description,omitempty"`
	// Members 列出 bundle 的成员资产，格式为 <type>/<asset-name>。
	Members []string `yaml:"members"`
}

// BundleMember 是解析后的 bundle 成员引用。
type BundleMember struct {
	// Type 取值 skill / command / rule / mcp。
	Type string
	// Name 是资产的短名（不含类型前缀）。
	Name string
}

// AssetRef 资产引用
type AssetRef struct {
	Name  string `yaml:"name"`
	Vault string `yaml:"vault"`
}

// TypedAssetRef 带类型信息的资产引用
type TypedAssetRef struct {
	Type       string
	Visibility AssetVisibility
	Plane      AssetPlane
	AssetRef
}

// VarsConfig 变量定义配置，用于占位符替换
type VarsConfig struct {
	Vars   map[string]string `yaml:"vars,omitempty"`
	Assets *AssetVars        `yaml:"assets,omitempty"`
}

// AssetVars 按资产类型和名称限定的变量
type AssetVars struct {
	MCPs     map[string]AssetVarEntry `yaml:"mcp,omitempty"`
	Rules    map[string]AssetVarEntry `yaml:"rule,omitempty"`
	Skills   map[string]AssetVarEntry `yaml:"skill,omitempty"`
	Commands map[string]AssetVarEntry `yaml:"command,omitempty"`
}

// AssetVarEntry 单个资产的变量覆盖
type AssetVarEntry struct {
	Vars map[string]string `yaml:"vars,omitempty"`
}
