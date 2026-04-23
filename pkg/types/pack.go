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

// ProjectConfig 项目配置 (<project>/.dec/config.yaml)
type ProjectConfig struct {
	Version        string     `yaml:"version,omitempty"`
	IDEs           []string   `yaml:"ides,omitempty"`
	Editor         string     `yaml:"editor,omitempty"`
	Available      *AssetList `yaml:"available,omitempty"`
	Enabled        *AssetList `yaml:"enabled,omitempty"`
	EnabledBundles []string   `yaml:"enabled_bundles,omitempty"`
}

// Bundle 描述 vault 内声明的一组资产启用单位。
//
// Bundle 的 YAML 声明位于 vault 根目录的 bundles/<name>.yaml：
//
//	name: vikunja
//	description: Vikunja 任务管理完整工作流
//	members:
//	  - mcp/vikunja-mcp
//	  - rules/vikunja-integration
//	  - skills/vikunja-workflow
//
// Bundle 不跨 vault，成员只能是 skill/rule/mcp（不能是 bundle）。
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
	// Type 取值 skill / rule / mcp，对应 AssetList 的三种资产类型。
	Type string
	// Name 是资产的短名（不含类型前缀）。
	Name string
}

// AssetList 资产列表（available 和 enabled 共用）
type AssetList struct {
	Skills []AssetRef `yaml:"skills,omitempty"`
	Rules  []AssetRef `yaml:"rules,omitempty"`
	MCPs   []AssetRef `yaml:"mcps,omitempty"`
}

// AssetRef 资产引用
type AssetRef struct {
	Name  string `yaml:"name"`
	Vault string `yaml:"vault"`
}

// Dedup 去重，同名资产以靠后的为准
func (l *AssetList) Dedup() {
	if l == nil {
		return
	}
	l.Skills = dedupRefs(l.Skills)
	l.Rules = dedupRefs(l.Rules)
	l.MCPs = dedupRefs(l.MCPs)
}

// IsEmpty 是否为空
func (l *AssetList) IsEmpty() bool {
	if l == nil {
		return true
	}
	return len(l.Skills) == 0 && len(l.Rules) == 0 && len(l.MCPs) == 0
}

// Count 资产总数
func (l *AssetList) Count() int {
	if l == nil {
		return 0
	}
	return len(l.Skills) + len(l.Rules) + len(l.MCPs)
}

// All 返回所有资产（带类型标记）
func (l *AssetList) All() []TypedAssetRef {
	if l == nil {
		return nil
	}
	var all []TypedAssetRef
	for _, s := range l.Skills {
		all = append(all, TypedAssetRef{Type: "skill", AssetRef: s})
	}
	for _, r := range l.Rules {
		all = append(all, TypedAssetRef{Type: "rule", AssetRef: r})
	}
	for _, m := range l.MCPs {
		all = append(all, TypedAssetRef{Type: "mcp", AssetRef: m})
	}
	return all
}

// Add 添加资产引用。
func (l *AssetList) Add(assetType string, ref AssetRef) {
	if l == nil {
		return
	}
	switch assetType {
	case "skill":
		l.Skills = append(l.Skills, ref)
	case "rule":
		l.Rules = append(l.Rules, ref)
	case "mcp":
		l.MCPs = append(l.MCPs, ref)
	}
}

// FindAsset 查找资产。可选传入 vault，仅匹配指定 Vault。
func (l *AssetList) FindAsset(assetType, name string, vault ...string) *AssetRef {
	if l == nil {
		return nil
	}
	var list []AssetRef
	targetVault := ""
	if len(vault) > 0 {
		targetVault = vault[0]
	}
	switch assetType {
	case "skill":
		list = l.Skills
	case "rule":
		list = l.Rules
	case "mcp":
		list = l.MCPs
	}
	for i := range list {
		if list[i].Name == name && (targetVault == "" || list[i].Vault == targetVault) {
			return &list[i]
		}
	}
	return nil
}

// RemoveAsset 移除资产。可选传入 vault，仅移除指定 Vault 中的资产。
func (l *AssetList) RemoveAsset(assetType, name string, vault ...string) bool {
	if l == nil {
		return false
	}
	targetVault := ""
	if len(vault) > 0 {
		targetVault = vault[0]
	}
	switch assetType {
	case "skill":
		for i, s := range l.Skills {
			if s.Name == name && (targetVault == "" || s.Vault == targetVault) {
				l.Skills = append(l.Skills[:i], l.Skills[i+1:]...)
				return true
			}
		}
	case "rule":
		for i, r := range l.Rules {
			if r.Name == name && (targetVault == "" || r.Vault == targetVault) {
				l.Rules = append(l.Rules[:i], l.Rules[i+1:]...)
				return true
			}
		}
	case "mcp":
		for i, m := range l.MCPs {
			if m.Name == name && (targetVault == "" || m.Vault == targetVault) {
				l.MCPs = append(l.MCPs[:i], l.MCPs[i+1:]...)
				return true
			}
		}
	}
	return false
}

// TypedAssetRef 带类型信息的资产引用
type TypedAssetRef struct {
	Type string
	AssetRef
}

// dedupRefs 去重，同名以靠后的为准
func dedupRefs(refs []AssetRef) []AssetRef {
	seen := make(map[string]int) // vault+name -> last index
	for i, r := range refs {
		seen[r.Vault+"\x00"+r.Name] = i
	}
	var result []AssetRef
	for i, r := range refs {
		if seen[r.Vault+"\x00"+r.Name] == i {
			result = append(result, r)
		}
	}
	return result
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
	Skills map[string]AssetVarEntry `yaml:"skill,omitempty"`
}

// AssetVarEntry 单个资产的变量覆盖
type AssetVarEntry struct {
	Vars map[string]string `yaml:"vars,omitempty"`
}
