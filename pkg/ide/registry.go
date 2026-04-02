package ide

import "sync"

// 已注册的 IDE 实现
var (
	registry = make(map[string]IDE)
	mu       sync.RWMutex
)

// Register 注册一个 IDE 实现
func Register(ide IDE) {
	mu.Lock()
	defer mu.Unlock()
	registry[ide.Name()] = ide
}

// Get 获取指定名称的 IDE 实现
// 如果不存在，返回一个基于通用实现的 IDE
func Get(name string) IDE {
	mu.RLock()
	defer mu.RUnlock()

	if ide, ok := registry[name]; ok {
		return ide
	}

	// 返回通用实现
	return &baseIDE{
		name:   name,
		dirKey: "." + name,
	}
}

// IsValid 检查指定名称的 IDE 是否已注册
func IsValid(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := registry[name]
	return ok
}

// List 列出所有已注册的 IDE 名称
func List() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// 初始化时注册内置的 IDE 实现
func init() {
	Register(&baseIDE{name: "cursor", dirKey: ".cursor"})
	// CodeBuddy 的 MCP 配置在根目录 .mcp.json
	Register(&baseIDE{name: "codebuddy", dirKey: ".codebuddy", mcpConfigPath: ".mcp.json"})
	Register(&baseIDE{name: "windsurf", dirKey: ".windsurf"})
	Register(&baseIDE{name: "trae", dirKey: ".trae"})
	Register(&baseIDE{name: "claude", dirKey: ".claude"})
	Register(&baseIDE{name: "claude-internal", dirKey: ".claude-internal"})
	Register(&baseIDE{name: "codex", dirKey: ".codex"})
	Register(&baseIDE{name: "codex-internal", dirKey: ".codex-internal"})
}
