package ide

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/types"
)

// MigrateLegacyClaudeProject 把旧版项目级 Claude Internal 布局迁移到当前约定。
//
// 迁移内容包括：
// 1. 把 .claude-internal/mcp.json 合并到 .claude/mcp.json
// 2. 把项目级 .claude-internal/{skills,rules} 挪到 .claude/{skills,rules}
func MigrateLegacyClaudeProject(projectRoot string) ([]string, error) {
	var notes []string

	legacyDir := filepath.Join(projectRoot, ".claude-internal")
	note, err := migrateLegacyClaudeMCPJSON(projectRoot, filepath.Join(legacyDir, "mcp.json"))
	if err != nil {
		return nil, err
	}
	if note != "" {
		notes = append(notes, note)
	}

	for _, pair := range []struct {
		src string
		dst string
	}{
		{src: filepath.Join(legacyDir, "skills"), dst: filepath.Join(projectRoot, ".claude", "skills")},
		{src: filepath.Join(legacyDir, "rules"), dst: filepath.Join(projectRoot, ".claude", "rules")},
	} {
		moved, err := migrateLegacyProjectDir(pair.src, pair.dst)
		if err != nil {
			return nil, err
		}
		if moved > 0 {
			notes = append(notes, fmt.Sprintf("%s -> %s (%d 项)", relProjectPath(projectRoot, pair.src), relProjectPath(projectRoot, pair.dst), moved))
		}
	}

	_ = removeDirIfEmpty(filepath.Join(legacyDir, "skills"))
	_ = removeDirIfEmpty(filepath.Join(legacyDir, "rules"))
	_ = removeDirIfEmpty(legacyDir)

	return notes, nil
}

func migrateLegacyClaudeMCPJSON(projectRoot, legacyPath string) (string, error) {
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	var legacy types.MCPConfig
	if err := json.Unmarshal(data, &legacy); err != nil {
		return "", fmt.Errorf("解析旧版 Claude MCP 配置失败 (%s): %w", legacyPath, err)
	}
	if legacy.MCPServers == nil {
		legacy.MCPServers = make(map[string]types.MCPServer)
	}

	targetPath := filepath.Join(projectRoot, ".claude", "mcp.json")
	current, err := loadJSONMCPConfig(targetPath)
	if err != nil {
		return "", err
	}
	for name, server := range legacy.MCPServers {
		if _, exists := current.MCPServers[name]; exists {
			continue
		}
		current.MCPServers[name] = server
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return "", err
	}
	merged, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(targetPath, merged, 0644); err != nil {
		return "", err
	}

	if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	_ = removeDirIfEmpty(filepath.Dir(legacyPath))

	return fmt.Sprintf("%s -> %s", relProjectPath(projectRoot, legacyPath), relProjectPath(projectRoot, targetPath)), nil
}

func loadJSONMCPConfig(path string) (*types.MCPConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.MCPConfig{MCPServers: make(map[string]types.MCPServer)}, nil
		}
		return nil, err
	}

	var config types.MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析 MCP 配置失败 (%s): %w", path, err)
	}
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]types.MCPServer)
	}

	return &config, nil
}
