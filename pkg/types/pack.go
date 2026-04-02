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
	RepoURL string `yaml:"repo_url,omitempty"`
}

// LocalConfig 本机全局配置 (~/.dec/local/config.yaml)
type LocalConfig struct {
	IDEs []string `yaml:"ides,omitempty"`
}

// ProjectConfig 项目配置 (<project>/.dec/config.yaml)
type ProjectConfig struct {
	Vaults []string `yaml:"vaults"`
	IDEs   []string `yaml:"ides,omitempty"`
}

// AssetsConfig 项目已安装资产追踪 (<project>/.dec/assets.yaml)
type AssetsConfig struct {
	Skills []AssetEntry `yaml:"skills,omitempty"`
	Rules  []AssetEntry `yaml:"rules,omitempty"`
	MCPs   []AssetEntry `yaml:"mcps,omitempty"`
}

// AssetEntry 单个已安装资产记录
type AssetEntry struct {
	Name        string            `yaml:"name"`
	Vault       string            `yaml:"vault"`
	InstalledAt string            `yaml:"installed_at"`
	VarsUsed    map[string]string `yaml:"vars_used,omitempty"`
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

// FindAsset 在资产列表中查找
func (a *AssetsConfig) FindAsset(assetType, name string) *AssetEntry {
	var list []AssetEntry
	switch assetType {
	case "skill":
		list = a.Skills
	case "rule":
		list = a.Rules
	case "mcp":
		list = a.MCPs
	}
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

// AddAsset 添加资产记录（去重），返回指向条目的指针以供调用方设置额外字段
func (a *AssetsConfig) AddAsset(assetType, name, vault, installedAt string) *AssetEntry {
	entry := AssetEntry{Name: name, Vault: vault, InstalledAt: installedAt}
	switch assetType {
	case "skill":
		a.Skills = addOrUpdateEntry(a.Skills, entry)
		return findEntryPtr(a.Skills, name)
	case "rule":
		a.Rules = addOrUpdateEntry(a.Rules, entry)
		return findEntryPtr(a.Rules, name)
	case "mcp":
		a.MCPs = addOrUpdateEntry(a.MCPs, entry)
		return findEntryPtr(a.MCPs, name)
	}
	return nil
}

// RemoveAsset 删除资产记录
func (a *AssetsConfig) RemoveAsset(assetType, name string) bool {
	switch assetType {
	case "skill":
		if n, ok := removeEntry(a.Skills, name); ok {
			a.Skills = n
			return true
		}
	case "rule":
		if n, ok := removeEntry(a.Rules, name); ok {
			a.Rules = n
			return true
		}
	case "mcp":
		if n, ok := removeEntry(a.MCPs, name); ok {
			a.MCPs = n
			return true
		}
	}
	return false
}

func addOrUpdateEntry(list []AssetEntry, entry AssetEntry) []AssetEntry {
	for i, e := range list {
		if e.Name == entry.Name {
			// 保留已有的 VarsUsed，除非新 entry 显式设置了
			if entry.VarsUsed == nil {
				entry.VarsUsed = e.VarsUsed
			}
			list[i] = entry
			return list
		}
	}
	return append(list, entry)
}

func removeEntry(list []AssetEntry, name string) ([]AssetEntry, bool) {
	for i, e := range list {
		if e.Name == name {
			return append(list[:i], list[i+1:]...), true
		}
	}
	return list, false
}

func findEntryPtr(list []AssetEntry, name string) *AssetEntry {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}
