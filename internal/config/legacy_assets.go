package config

import (
	"gopkg.in/yaml.v3"
)

// legacyAssetSection 解析已废弃的 available / enabled 段，只保留其中出现过的 vault 名。
//
// Dec 早期支持单资产粒度的启用，配置里有两种历史形态：
//
//	v1（列表）                      v2（vault -> asset -> type 嵌套映射）
//	enabled:                        enabled:
//	  skills:                         vikunja:
//	    - name: vikunja-workflow        vikunja-workflow:
//	      vault: vikunja                  skills: true
//
// 两种形态里 vault 名都等价于 bundle 目录名，因此迁移时只需要 vault 集合：
// 把它们折叠成 enabled_bundles 即可保留用户「我要这些资产」的意图。
type legacyAssetSection struct {
	// Vaults 按出现顺序记录，去重在 foldLegacyBundles 里统一做。
	Vaults []string
}

func (s *legacyAssetSection) UnmarshalYAML(node *yaml.Node) error {
	if s == nil || node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]

		if isLegacyTypeKey(key) && value.Kind == yaml.SequenceNode {
			for _, item := range value.Content {
				var ref struct {
					Vault string `yaml:"vault"`
				}
				if err := item.Decode(&ref); err == nil && ref.Vault != "" {
					s.Vaults = append(s.Vaults, ref.Vault)
				}
			}
			continue
		}

		if key != "" {
			s.Vaults = append(s.Vaults, key)
		}
	}
	return nil
}

func isLegacyTypeKey(key string) bool {
	switch key {
	case "skills", "commands", "rules", "mcps":
		return true
	default:
		return false
	}
}

// legacyProjectAssets 用于从原始 YAML 里嗅探是否还残留 available / enabled 段。
type legacyProjectAssets struct {
	Available *legacyAssetSection `yaml:"available"`
	Enabled   *legacyAssetSection `yaml:"enabled"`
}

func (l legacyProjectAssets) present() bool {
	return l.Available != nil || l.Enabled != nil
}

// foldLegacyBundles 把 legacy enabled 段里的 vault 折叠进 bundle 列表。
//
// available 段只是一份仓库扫描快照，不表达用户意图，直接丢弃。
// 已在 existing 里的 bundle 保持原有顺序在前，新折叠出来的追加在后。
func foldLegacyBundles(existing []string, enabled *legacyAssetSection) []string {
	seen := make(map[string]struct{}, len(existing))
	out := make([]string, 0, len(existing))
	for _, name := range existing {
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if enabled == nil {
		return out
	}
	for _, vault := range enabled.Vaults {
		if vault == "" {
			continue
		}
		if _, dup := seen[vault]; dup {
			continue
		}
		seen[vault] = struct{}{}
		out = append(out, vault)
	}
	return out
}
