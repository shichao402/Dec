package types

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

// GlobalConfig 全局配置结构 (~/.dec/config.yaml)
type GlobalConfig struct {
	RepoURL string   `yaml:"repo_url,omitempty"`
	IDEs    []string `yaml:"ides,omitempty"`
	Editor  string   `yaml:"editor,omitempty"`
}

const ProjectConfigVersionV2 = "v2"

// VaultProjectsDir 是 Git Vault 中 project 声明目录。
const VaultProjectsDir = "projects"

// VaultBundlesDir 是 Git Vault 中 bundle 根目录。
const VaultBundlesDir = "bundles"

// BundleManifestFileName 是每个 bundle 目录内的声明文件名。
const BundleManifestFileName = "bundle.yaml"

// VaultProjectFileExt 是 project 声明文件扩展名。
const VaultProjectFileExt = ".yaml"

// Project 描述 vault 中 projects/<name>.yaml 的项目声明。
//
// Wire format 示例（projects/my-app.yaml）：
//
//	name: my-app
//	description: 我的应用项目
//	bundles:
//	  - vikunja
//	  - helloworld
//	ides:
//	  - cursor
type Project struct {
	// Name 为 project 短名，与文件名 projects/<name>.yaml 一致。
	Name string `yaml:"name"`
	// Description 是 TUI 展示用的一句话描述。
	Description string `yaml:"description,omitempty"`
	// Bundles 列出该项目启用的 Dec bundle 短名（对应 bundles/<name>/）。
	Bundles []string `yaml:"bundles"`
	// IDEs 为该项目默认 IDE 列表；本地 .dec/config.yaml 可覆盖。
	IDEs []string `yaml:"ides,omitempty"`
	// Editor 为该项目默认交互式编辑器；本地可覆盖。
	Editor string `yaml:"editor,omitempty"`
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

// ProjectConfig 项目配置 (<project>/.dec/config.yaml)
type ProjectConfig struct {
	Version string `yaml:"version,omitempty"`
	// ProjectName 引用 vault projects/<project_name>.yaml。
	// 未显式设置时，调用方应 fallback 到 filepath.Base(projectRoot)，但不会自动写回 yaml。
	ProjectName string   `yaml:"project_name,omitempty"`
	IDEs        []string `yaml:"ides,omitempty"`
	Editor      string   `yaml:"editor,omitempty"`
	// EnabledBundles 是本项目启用的 bundle 短名列表，也是唯一的资产启用入口。
	// 早期版本支持的单资产粒度（available / enabled）已移除，加载旧配置时会折叠成 bundle 引用。
	EnabledBundles []string `yaml:"enabled_bundles,omitempty"`
}

// Bundle 描述 vault 内声明的一组资产启用单位。
//
// Bundle 的 YAML 声明位于 bundles/<name>/bundle.yaml：
//
//	name: vikunja
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
	Type string
	AssetRef
}

// VarsConfig 变量定义配置，用于占位符替换
type VarsConfig struct {
	Vars   map[string]string `yaml:"vars,omitempty"`
	Assets *AssetVars        `yaml:"assets,omitempty"`
}

// AssetVars 按资产类型和名称限定的变量
type AssetVars struct {
	MCPs   map[string]AssetVarEntry `yaml:"mcp,omitempty"`
	Rules  map[string]AssetVarEntry `yaml:"rule,omitempty"`
	Skills   map[string]AssetVarEntry `yaml:"skill,omitempty"`
	Commands map[string]AssetVarEntry `yaml:"command,omitempty"`
}

// AssetVarEntry 单个资产的变量覆盖
type AssetVarEntry struct {
	Vars map[string]string `yaml:"vars,omitempty"`
}
