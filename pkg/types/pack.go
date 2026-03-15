package types

// IDEsConfig 表示 IDE 配置（ides.yaml）
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
