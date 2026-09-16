package types

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const RequiresLatest = "latest"

// RequiresSpec 是消费声明：提供方项目名 → latest 或提供方版本（如 v0.3.20）。
type RequiresSpec map[string]string

func (r *RequiresSpec) UnmarshalYAML(value *yaml.Node) error {
	if r == nil {
		return fmt.Errorf("requires 不能为 nil")
	}
	if value == nil || value.Kind == yaml.ScalarNode && strings.TrimSpace(value.Value) == "" {
		*r = nil
		return nil
	}
	if value.Kind == yaml.SequenceNode {
		return fmt.Errorf("requires 必须是 map（项目: latest 或版本），不再接受列表")
	}
	var m map[string]string
	if err := value.Decode(&m); err != nil {
		return fmt.Errorf("解析 requires 失败: %w", err)
	}
	*r = m
	return nil
}

func (r RequiresSpec) Has(name string) bool {
	if len(r) == 0 {
		return false
	}
	_, ok := r[strings.TrimSpace(name)]
	return ok
}

func (r RequiresSpec) MarshalYAML() (any, error) {
	if len(r) == 0 {
		return map[string]string(nil), nil
	}
	out := make(map[string]string, len(r))
	for k, v := range r {
		out[k] = v
	}
	return out, nil
}

// NormalizeRequiresSpec 校验并规范化。空值视为 latest。
func NormalizeRequiresSpec(in RequiresSpec) (RequiresSpec, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(RequiresSpec, len(in))
	for rawName, rawVer := range in {
		name := strings.TrimSpace(rawName)
		if !IsValidProjectName(name) {
			return nil, fmt.Errorf("requires 项目名 %q 非法", rawName)
		}
		ver := strings.TrimSpace(rawVer)
		if ver == "" {
			ver = RequiresLatest
		}
		if strings.ContainsAny(ver, "/\\ \t") {
			return nil, fmt.Errorf("requires.%s 版本 %q 非法", name, rawVer)
		}
		if ver != RequiresLatest && !strings.HasPrefix(ver, "v") {
			return nil, fmt.Errorf("requires.%s 版本 %q 必须是 latest 或 v 开头的提供方 tag", name, rawVer)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("requires 重复项目 %q", name)
		}
		out[name] = ver
	}
	return out, nil
}
