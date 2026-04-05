package types

// IDEsConfig 表示 IDE 配置
type IDEsConfig struct {
	IDEs []string `yaml:"ides,omitempty" json:"ides,omitempty"`
}

// MCPConfig 表示 MCP 配置文件（.cursor/mcp.json）
type MCPConfig struct {
	MCPServers map[string]MCPServer `json:"mcpServers"`
}

// MCPServer 表示单个 MCP Server 配置
type MCPServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// GlobalConfig 全局配置结构 (~/.dec/config.yaml)
type GlobalConfig struct {
	RepoURL string   `yaml:"repo_url,omitempty"`
	IDEs    []string `yaml:"ides,omitempty"`
	Editor  string   `yaml:"editor,omitempty"`
}

// ProjectConfig 项目配置 (<project>/.dec/config.yaml)
type ProjectConfig struct {
	IDEs      []string   `yaml:"ides,omitempty"`
	Editor    string     `yaml:"editor,omitempty"`
	Available *AssetList `yaml:"available,omitempty"`
	Enabled   *AssetList `yaml:"enabled,omitempty"`
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

// FindAsset 查找资产
func (l *AssetList) FindAsset(assetType, name string) *AssetRef {
	if l == nil {
		return nil
	}
	var list []AssetRef
	switch assetType {
	case "skill":
		list = l.Skills
	case "rule":
		list = l.Rules
	case "mcp":
		list = l.MCPs
	}
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

// RemoveAsset 移除资产
func (l *AssetList) RemoveAsset(assetType, name string) bool {
	if l == nil {
		return false
	}
	switch assetType {
	case "skill":
		for i, s := range l.Skills {
			if s.Name == name {
				l.Skills = append(l.Skills[:i], l.Skills[i+1:]...)
				return true
			}
		}
	case "rule":
		for i, r := range l.Rules {
			if r.Name == name {
				l.Rules = append(l.Rules[:i], l.Rules[i+1:]...)
				return true
			}
		}
	case "mcp":
		for i, m := range l.MCPs {
			if m.Name == name {
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
	seen := make(map[string]int) // name -> last index
	for i, r := range refs {
		seen[r.Name] = i
	}
	var result []AssetRef
	for i, r := range refs {
		if seen[r.Name] == i {
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
