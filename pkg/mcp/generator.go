package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/types"
)

// Generator MCP 配置生成器
type Generator struct {
	projectRoot string
	ide         string // 目标 IDE
}

// NewGenerator 创建 MCP 配置生成器（默认 Cursor）
func NewGenerator(projectRoot string) *Generator {
	return &Generator{projectRoot: projectRoot, ide: "cursor"}
}

// NewGeneratorForIDE 创建指定 IDE 的 MCP 配置生成器
func NewGeneratorForIDE(projectRoot, ide string) *Generator {
	return &Generator{projectRoot: projectRoot, ide: ide}
}

// GenerateConfig 生成 MCP 配置文件
func (g *Generator) GenerateConfig(packs []MCPPackInfo) error {
	config := types.MCPConfig{
		MCPServers: make(map[string]types.MCPServer),
	}

	for _, pack := range packs {
		server, err := g.buildMCPServer(pack)
		if err != nil {
			return fmt.Errorf("构建 MCP Server 配置失败 (%s): %w", pack.Name, err)
		}
		config.MCPServers[pack.Name] = *server
	}

	return g.writeConfig(&config)
}

// MCPPackInfo MCP 包信息
type MCPPackInfo struct {
	Name        string                 // 包名
	InstallPath string                 // 安装路径
	LocalPath   string                 // 本地开发路径（如果是链接的包）
	Pack        *types.Pack            // 包元数据
	UserConfig  map[string]interface{} // 用户配置
}

// buildMCPServer 构建单个 MCP Server 配置
func (g *Generator) buildMCPServer(pack MCPPackInfo) (*types.MCPServer, error) {
	if pack.Pack == nil || pack.Pack.MCP == nil {
		return nil, fmt.Errorf("包 %s 缺少 MCP 配置", pack.Name)
	}

	mcpConfig := pack.Pack.MCP

	// 确定包的实际路径
	packPath := pack.InstallPath
	if pack.LocalPath != "" {
		packPath = pack.LocalPath
	}

	// 构建命令路径
	command := mcpConfig.Command
	if !filepath.IsAbs(command) {
		command = filepath.Join(packPath, command)
	}

	// 处理环境变量模板
	env := make(map[string]string)
	for key, value := range mcpConfig.Env {
		// 替换 ${VAR} 格式的环境变量引用
		env[key] = value
	}

	// 应用用户配置
	if pack.UserConfig != nil {
		// 如果用户配置了 token_env，使用用户指定的环境变量名
		if tokenEnv, ok := pack.UserConfig["token_env"].(string); ok {
			for key := range env {
				if key == "GITHUB_TOKEN" || key == "TOKEN" {
					env[key] = fmt.Sprintf("${%s}", tokenEnv)
				}
			}
		}
	}

	return &types.MCPServer{
		Command: command,
		Args:    mcpConfig.Args,
		Env:     env,
	}, nil
}

// writeConfig 写入 MCP 配置文件
func (g *Generator) writeConfig(config *types.MCPConfig) error {
	configPath := paths.GetIDEMCPConfigPath(g.projectRoot, g.ide)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 如果配置为空，不写入文件
	if len(config.MCPServers) == 0 {
		// 如果文件存在，删除它
		if _, err := os.Stat(configPath); err == nil {
			return os.Remove(configPath)
		}
		return nil
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	return os.WriteFile(configPath, data, 0644)
}

// LoadExistingConfig 加载现有的 MCP 配置
func (g *Generator) LoadExistingConfig() (*types.MCPConfig, error) {
	configPath := paths.GetIDEMCPConfigPath(g.projectRoot, g.ide)

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.MCPConfig{
				MCPServers: make(map[string]types.MCPServer),
			}, nil
		}
		return nil, err
	}

	var config types.MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if config.MCPServers == nil {
		config.MCPServers = make(map[string]types.MCPServer)
	}

	return &config, nil
}

// MergeConfig 合并配置（保留用户手动添加的配置）
func (g *Generator) MergeConfig(existing *types.MCPConfig, generated *types.MCPConfig, managedPacks []string) *types.MCPConfig {
	result := &types.MCPConfig{
		MCPServers: make(map[string]types.MCPServer),
	}

	// 创建 managed packs 集合
	managed := make(map[string]bool)
	for _, name := range managedPacks {
		managed[name] = true
	}

	// 保留非托管的配置
	for name, server := range existing.MCPServers {
		if !managed[name] {
			result.MCPServers[name] = server
		}
	}

	// 添加生成的配置
	for name, server := range generated.MCPServers {
		result.MCPServers[name] = server
	}

	return result
}

// GenerateDecServer 生成 Dec 自身的 MCP Server 配置
func (g *Generator) GenerateDecServer() types.MCPServer {
	return types.MCPServer{
		Command: "dec",
		Args:    []string{"serve"},
	}
}
