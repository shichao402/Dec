package loader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/firoyang/CursorToolset/pkg/types"
)

// LoadToolsets 从 toolsets.json 加载工具集列表
func LoadToolsets(toolsetsPath string) ([]*types.ToolsetInfo, error) {
	data, err := os.ReadFile(toolsetsPath)
	if err != nil {
		return nil, fmt.Errorf("读取 toolsets.json 失败: %w", err)
	}
	
	var toolsets []*types.ToolsetInfo
	if err := json.Unmarshal(data, &toolsets); err != nil {
		return nil, fmt.Errorf("解析 toolsets.json 失败: %w", err)
	}
	
	return toolsets, nil
}

// FindToolset 根据名称查找工具集
func FindToolset(toolsets []*types.ToolsetInfo, name string) *types.ToolsetInfo {
	for _, toolset := range toolsets {
		if toolset.Name == name {
			return toolset
		}
	}
	return nil
}

// GetToolsetsPath 获取 toolsets.json 的路径
func GetToolsetsPath(workDir string) string {
	// 首先尝试工作目录
	path := filepath.Join(workDir, "toolsets.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	
	// 尝试项目根目录
	path = filepath.Join(workDir, "..", "toolsets.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	
	// 返回默认路径
	return filepath.Join(workDir, "toolsets.json")
}

