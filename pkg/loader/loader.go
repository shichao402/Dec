package loader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/firoyang/CursorToolset/pkg/types"
)

// ToolsetSearchResult 表示搜索结果
type ToolsetSearchResult struct {
	Toolset       *types.ToolsetInfo
	MatchedFields []string // 匹配的字段名称
}

// LoadToolsets 从 available-toolsets.json 加载工具集列表
func LoadToolsets(toolsetsPath string) ([]*types.ToolsetInfo, error) {
	data, err := os.ReadFile(toolsetsPath)
	if err != nil {
		return nil, fmt.Errorf("读取 available-toolsets.json 失败: %w", err)
	}
	
	var toolsets []*types.ToolsetInfo
	if err := json.Unmarshal(data, &toolsets); err != nil {
		return nil, fmt.Errorf("解析 available-toolsets.json 失败: %w", err)
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

// SearchToolset 搜索工具集，返回匹配结果
func SearchToolset(toolset *types.ToolsetInfo, keyword string) *ToolsetSearchResult {
	keyword = strings.ToLower(keyword)
	var matchedFields []string
	
	// 搜索名称
	if strings.Contains(strings.ToLower(toolset.Name), keyword) {
		matchedFields = append(matchedFields, "名称")
	}
	
	// 搜索显示名称
	if strings.Contains(strings.ToLower(toolset.DisplayName), keyword) {
		matchedFields = append(matchedFields, "显示名称")
	}
	
	// 搜索描述
	if strings.Contains(strings.ToLower(toolset.Description), keyword) {
		matchedFields = append(matchedFields, "描述")
	}
	
	// 搜索 URL（可能包含项目名）
	if strings.Contains(strings.ToLower(toolset.GitHubURL), keyword) {
		matchedFields = append(matchedFields, "仓库地址")
	}
	
	if len(matchedFields) > 0 {
		return &ToolsetSearchResult{
			Toolset:       toolset,
			MatchedFields: matchedFields,
		}
	}
	
	return nil
}

// GetToolsetsPath 获取 available-toolsets.json 的路径
func GetToolsetsPath(workDir string) string {
	// 首先尝试工作目录
	path := filepath.Join(workDir, "available-toolsets.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	
	// 尝试项目根目录
	path = filepath.Join(workDir, "..", "available-toolsets.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	
	// 返回默认路径
	return filepath.Join(workDir, "available-toolsets.json")
}



