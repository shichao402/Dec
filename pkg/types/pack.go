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
	Name        string `yaml:"name"`
	Vault       string `yaml:"vault"`
	InstalledAt string `yaml:"installed_at"`
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

// AddAsset 添加资产记录（去重）
func (a *AssetsConfig) AddAsset(assetType, name, vault, installedAt string) {
	entry := AssetEntry{Name: name, Vault: vault, InstalledAt: installedAt}
	switch assetType {
	case "skill":
		a.Skills = addOrUpdateEntry(a.Skills, entry)
	case "rule":
		a.Rules = addOrUpdateEntry(a.Rules, entry)
	case "mcp":
		a.MCPs = addOrUpdateEntry(a.MCPs, entry)
	}
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
