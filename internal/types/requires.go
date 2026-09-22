package types

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const RequiresLatest = "latest"

// RequiresVault 是个人私仓 pin：私仓是单分支可变仓，没有版本，只能跟随 HEAD（ADR 0029）。
// 只用于注册表未发布的个人项目。已发布项目不能再 pin 成 vault（ADR 0031）。
const RequiresVault = "vault"

// RequiresSpec 是唯一的消费声明：提供方项目名 → pin。
// pin 为 latest 或精确 v* 时解析官方注册表，为 vault 时解析个人私仓。
type RequiresSpec map[string]string

// IsVaultPin 判断 pin 是否指向个人私仓。
func IsVaultPin(pin string) bool {
	return strings.TrimSpace(pin) == RequiresVault
}

// VaultProjects 返回按名字排序的个人私仓订阅。
func (r RequiresSpec) VaultProjects() []string {
	out := make([]string, 0, len(r))
	for name, pin := range r {
		if IsVaultPin(pin) {
			out = append(out, strings.TrimSpace(name))
		}
	}
	sort.Strings(out)
	return out
}

// OfficialProjects 返回按名字排序的官方注册表订阅。
func (r RequiresSpec) OfficialProjects() []string {
	out := make([]string, 0, len(r))
	for name, pin := range r {
		if !IsVaultPin(pin) {
			out = append(out, strings.TrimSpace(name))
		}
	}
	sort.Strings(out)
	return out
}

// Official 只保留官方注册表订阅，供 install 解析 tag。
func (r RequiresSpec) Official() RequiresSpec {
	if len(r) == 0 {
		return nil
	}
	out := make(RequiresSpec, len(r))
	for name, pin := range r {
		if !IsVaultPin(pin) {
			out[strings.TrimSpace(name)] = strings.TrimSpace(pin)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FoldPublishedVaultPins 把已发布项目上残留的 vault pin 收成 latest。
// 家项目保持原样：它从工作树创作，不进注册表订阅。
// published 为空表示这次没能确认注册表，调用方不得猜测，原样返回。
func (r RequiresSpec) FoldPublishedVaultPins(published map[string]struct{}, home string) RequiresSpec {
	if len(r) == 0 || len(published) == 0 {
		return r
	}
	home = strings.TrimSpace(home)
	out := make(RequiresSpec, len(r))
	changed := false
	for name, pin := range r {
		if name != home && IsVaultPin(pin) {
			if _, ok := published[name]; ok {
				out[name] = RequiresLatest
				changed = true
				continue
			}
		}
		out[name] = pin
	}
	if !changed {
		return r
	}
	return out
}

// IsVault 判断某个项目是否按私仓 pin 订阅。
func (r RequiresSpec) IsVault(name string) bool {
	if len(r) == 0 {
		return false
	}
	return IsVaultPin(r[strings.TrimSpace(name)])
}

// AddVaultProjects 把私仓项目名并入声明，已存在的项目保持原 pin 不变。
func (r RequiresSpec) AddVaultProjects(names []string) RequiresSpec {
	out := make(RequiresSpec, len(r)+len(names))
	for name, pin := range r {
		out[strings.TrimSpace(name)] = strings.TrimSpace(pin)
	}
	for _, raw := range names {
		name := strings.TrimSpace(strings.TrimPrefix(raw, "bundle/"))
		if name == "" || !IsValidProjectName(name) {
			continue
		}
		if _, ok := out[name]; ok {
			continue
		}
		out[name] = RequiresVault
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

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
		if ver != RequiresLatest && ver != RequiresVault && !strings.HasPrefix(ver, "v") {
			return nil, fmt.Errorf("requires.%s pin %q 必须是 latest、vault 或 v 开头的提供方 tag", name, rawVer)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("requires 重复项目 %q", name)
		}
		out[name] = ver
	}
	return out, nil
}
