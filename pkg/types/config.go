package types

import (
	"gopkg.in/yaml.v3"
)

// ========================================
// 新配置格式类型
// 支持两种格式：
// - 简单格式: "go"
// - 带变量格式: { "flutter": { "state_management": "Provider" } }
// ========================================

// ConfigItem 表示配置项（可以是字符串或带变量的对象）
type ConfigItem struct {
	Name string                 // 配置项名称
	Vars map[string]interface{} // 变量配置（可选）
}

// UnmarshalYAML 自定义 YAML 解析
func (c *ConfigItem) UnmarshalYAML(value *yaml.Node) error {
	// 尝试解析为字符串
	if value.Kind == yaml.ScalarNode {
		c.Name = value.Value
		c.Vars = nil
		return nil
	}

	// 尝试解析为 map（带变量的格式）
	if value.Kind == yaml.MappingNode {
		// 格式: { "flutter": { "state_management": "Provider" } }
		// 只有一个 key，key 是名称，value 是变量
		if len(value.Content) >= 2 {
			c.Name = value.Content[0].Value
			c.Vars = make(map[string]interface{})

			// 解析变量
			varsNode := value.Content[1]
			if varsNode.Kind == yaml.MappingNode {
				for i := 0; i < len(varsNode.Content); i += 2 {
					key := varsNode.Content[i].Value
					var val interface{}
					if err := varsNode.Content[i+1].Decode(&val); err != nil {
						return err
					}
					c.Vars[key] = val
				}
			}
		}
		return nil
	}

	return nil
}

// MarshalYAML 自定义 YAML 序列化
func (c ConfigItem) MarshalYAML() (interface{}, error) {
	if len(c.Vars) == 0 {
		return c.Name, nil
	}
	return map[string]interface{}{c.Name: c.Vars}, nil
}

// NewTechnologyConfigV2 新版技术栈配置
type NewTechnologyConfigV2 struct {
	Languages  []ConfigItem `yaml:"languages,omitempty"`
	Frameworks []ConfigItem `yaml:"frameworks,omitempty"`
	Platforms  []ConfigItem `yaml:"platforms,omitempty"`
	Patterns   []ConfigItem `yaml:"patterns,omitempty"`
	// 扩展分类（ext-* 前缀）
	Extensions map[string][]ConfigItem `yaml:",inline"`
}

// NewMCPConfigV2 新版 MCP 配置
type NewMCPConfigV2 struct {
	MCPs []ConfigItem `yaml:"mcps,omitempty"`
}

// GetEnabledNames 获取所有启用的名称列表
func (c *NewTechnologyConfigV2) GetEnabledNames(category string) []string {
	var items []ConfigItem
	switch category {
	case "languages":
		items = c.Languages
	case "frameworks":
		items = c.Frameworks
	case "platforms":
		items = c.Platforms
	case "patterns":
		items = c.Patterns
	default:
		// 检查扩展分类
		if c.Extensions != nil {
			items = c.Extensions[category]
		}
	}

	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}

// GetItemVars 获取指定配置项的变量
func (c *NewTechnologyConfigV2) GetItemVars(category, name string) map[string]interface{} {
	var items []ConfigItem
	switch category {
	case "languages":
		items = c.Languages
	case "frameworks":
		items = c.Frameworks
	case "platforms":
		items = c.Platforms
	case "patterns":
		items = c.Patterns
	default:
		if c.Extensions != nil {
			items = c.Extensions[category]
		}
	}

	for _, item := range items {
		if item.Name == name {
			return item.Vars
		}
	}
	return nil
}
