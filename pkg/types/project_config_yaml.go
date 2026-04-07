package types

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	projectConfigKeySkills = "skills"
	projectConfigKeyRules  = "rules"
	projectConfigKeyMCP    = "mcp"
)

type assetTypeSet struct {
	Skill bool
	Rule  bool
	MCP   bool
}

// MarshalYAML 将资产列表编码为 v2 的 vault -> item -> type 结构。
func (l *AssetList) MarshalYAML() (interface{}, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}
	if l == nil {
		return root, nil
	}

	grouped := make(map[string]map[string]*assetTypeSet)
	for _, asset := range l.All() {
		items, ok := grouped[asset.Vault]
		if !ok {
			items = make(map[string]*assetTypeSet)
			grouped[asset.Vault] = items
		}
		set, ok := items[asset.Name]
		if !ok {
			set = &assetTypeSet{}
			items[asset.Name] = set
		}
		switch asset.Type {
		case "skill":
			set.Skill = true
		case "rule":
			set.Rule = true
		case "mcp":
			set.MCP = true
		}
	}

	vaults := sortedKeys(grouped)
	for _, vault := range vaults {
		root.Content = append(root.Content, scalarNode(vault))

		vaultNode := &yaml.Node{Kind: yaml.MappingNode}
		items := grouped[vault]
		for _, itemName := range sortedKeys(items) {
			vaultNode.Content = append(vaultNode.Content, scalarNode(itemName))

			typesNode := &yaml.Node{Kind: yaml.MappingNode}
			set := items[itemName]
			if set.Skill {
				typesNode.Content = append(typesNode.Content, scalarNode(projectConfigKeySkills), boolNode(true))
			}
			if set.Rule {
				typesNode.Content = append(typesNode.Content, scalarNode(projectConfigKeyRules), boolNode(true))
			}
			if set.MCP {
				typesNode.Content = append(typesNode.Content, scalarNode(projectConfigKeyMCP), boolNode(true))
			}

			vaultNode.Content = append(vaultNode.Content, typesNode)
		}

		root.Content = append(root.Content, vaultNode)
	}

	return root, nil
}

// UnmarshalYAML 解析 v2 的 vault -> item -> type 结构。
func (l *AssetList) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == 0 {
		*l = AssetList{}
		return nil
	}
	if value.Kind == yaml.ScalarNode && value.Tag == "!!null" {
		*l = AssetList{}
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("应为映射结构")
	}

	result := AssetList{}
	for i := 0; i < len(value.Content); i += 2 {
		vault := value.Content[i].Value
		itemsNode := value.Content[i+1]
		if itemsNode.Kind != yaml.MappingNode {
			return fmt.Errorf("vault %q 应为映射结构", vault)
		}

		for j := 0; j < len(itemsNode.Content); j += 2 {
			itemName := itemsNode.Content[j].Value
			typesNode := itemsNode.Content[j+1]
			if typesNode.Kind != yaml.MappingNode {
				return fmt.Errorf("资产 %q/%q 应为映射结构", vault, itemName)
			}

			for k := 0; k < len(typesNode.Content); k += 2 {
				rawType := typesNode.Content[k].Value
				assetType, ok := normalizeProjectAssetTypeKey(rawType)
				if !ok {
					return fmt.Errorf("资产 %q/%q 包含不支持的类型键 %q", vault, itemName, rawType)
				}

				enabled, err := decodeAssetTypePresence(typesNode.Content[k+1])
				if err != nil {
					return fmt.Errorf("解析资产 %q/%q 的类型 %q 失败: %w", vault, itemName, rawType, err)
				}
				if !enabled {
					continue
				}

				result.addAsset(assetType, AssetRef{Name: itemName, Vault: vault})
			}
		}
	}

	result.Dedup()
	*l = result
	return nil
}

func (l *AssetList) addAsset(assetType string, ref AssetRef) {
	switch assetType {
	case "skill":
		l.Skills = append(l.Skills, ref)
	case "rule":
		l.Rules = append(l.Rules, ref)
	case "mcp":
		l.MCPs = append(l.MCPs, ref)
	}
}

func normalizeProjectAssetTypeKey(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "skill", projectConfigKeySkills:
		return "skill", true
	case "rule", projectConfigKeyRules:
		return "rule", true
	case "mcp", "mcps":
		return "mcp", true
	default:
		return "", false
	}
}

func decodeAssetTypePresence(node *yaml.Node) (bool, error) {
	if node.Kind == yaml.MappingNode {
		if len(node.Content) == 0 {
			return true, nil
		}
		return false, fmt.Errorf("应为 true/false、留空或空映射")
	}
	if node.Kind != yaml.ScalarNode {
		return false, fmt.Errorf("应为标量或空映射")
	}
	if node.Tag == "!!null" {
		return true, nil
	}

	var enabled bool
	if err := node.Decode(&enabled); err != nil {
		return false, fmt.Errorf("应为 true/false")
	}
	return enabled, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func scalarNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func boolNode(v bool) *yaml.Node {
	value := "false"
	if v {
		value = "true"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: value}
}
