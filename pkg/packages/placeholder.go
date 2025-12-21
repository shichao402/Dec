package packages

import (
	"fmt"
	"regexp"
	"strings"
)

// Placeholder 表示一个占位符
type Placeholder struct {
	Raw        string // 原始占位符文本，如 {{path.to.var:-default}}
	Path       string // 变量路径，如 path.to.var
	Default    string // 默认值，如 default
	HasDefault bool   // 是否有默认值
}

// PlaceholderParser 占位符解析器
type PlaceholderParser struct {
	pattern *regexp.Regexp
}

// NewPlaceholderParser 创建占位符解析器
func NewPlaceholderParser() *PlaceholderParser {
	// 匹配 {{xxx}} 或 {{xxx:-yyy}}
	pattern := regexp.MustCompile(`\{\{([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)*)(?::-([^}]*))?\}\}`)
	return &PlaceholderParser{pattern: pattern}
}

// Parse 解析内容中的所有占位符
func (p *PlaceholderParser) Parse(content string) []Placeholder {
	matches := p.pattern.FindAllStringSubmatch(content, -1)

	var placeholders []Placeholder
	seen := make(map[string]bool)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		raw := match[0]
		path := match[1]

		// 去重
		if seen[path] {
			continue
		}
		seen[path] = true

		ph := Placeholder{
			Raw:  raw,
			Path: path,
		}

		if len(match) >= 3 && match[2] != "" {
			ph.Default = match[2]
			ph.HasDefault = true
		}

		placeholders = append(placeholders, ph)
	}

	return placeholders
}

// Replace 替换内容中的占位符
func (p *PlaceholderParser) Replace(content string, vars map[string]interface{}) string {
	result := content

	placeholders := p.Parse(content)
	for _, ph := range placeholders {
		value := p.resolveValue(ph.Path, vars)
		if value == "" && ph.HasDefault {
			value = ph.Default
		}
		result = strings.ReplaceAll(result, ph.Raw, value)
	}

	return result
}

// resolveValue 从变量 map 中解析路径值
func (p *PlaceholderParser) resolveValue(path string, vars map[string]interface{}) string {
	if vars == nil {
		return ""
	}

	parts := strings.Split(path, ".")
	var current interface{} = vars

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			val, ok := v[part]
			if !ok {
				return ""
			}
			current = val
		default:
			return ""
		}
	}

	// 转换为字符串
	switch v := current.(type) {
	case string:
		return v
	case int, int64, float64, bool:
		return fmt.Sprintf("%v", v)
	default:
		return ""
	}
}

// ExtractDefaultVars 从占位符列表中提取默认变量配置
// 返回嵌套的 map 结构
func ExtractDefaultVars(placeholders []Placeholder) map[string]interface{} {
	result := make(map[string]interface{})

	for _, ph := range placeholders {
		if !ph.HasDefault {
			continue
		}

		parts := strings.Split(ph.Path, ".")
		current := result

		// 创建嵌套结构
		for i, part := range parts {
			if i == len(parts)-1 {
				// 最后一个部分，设置值
				current[part] = ph.Default
			} else {
				// 中间部分，创建嵌套 map
				if _, ok := current[part]; !ok {
					current[part] = make(map[string]interface{})
				}
				if nested, ok := current[part].(map[string]interface{}); ok {
					current = nested
				}
			}
		}
	}

	return result
}
