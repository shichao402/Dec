package types

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAssetListMarshalYAML_V2VaultFirstShape(t *testing.T) {
	list := &AssetList{
		Skills: []AssetRef{{Name: "api-test", Vault: "team"}},
		Rules:  []AssetRef{{Name: "api-test", Vault: "team"}, {Name: "lint", Vault: "infra"}},
		MCPs:   []AssetRef{{Name: "postgres", Vault: "infra"}},
	}

	data, err := yaml.Marshal(list)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	content := string(data)

	checks := []string{
		"team:",
		"api-test:",
		"skills: true",
		"rules: true",
		"infra:",
		"postgres:",
		"mcp: true",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Fatalf("序列化结果缺少 %q:\n%s", check, content)
		}
	}
	if strings.Contains(content, "- name:") {
		t.Fatalf("v2 序列化不应输出旧列表结构:\n%s", content)
	}
}

func TestAssetListUnmarshalYAML_V2VaultFirstShape(t *testing.T) {
	raw := `
team:
  shared:
    skills: true
    rules: true
infra:
  postgres:
    mcp: true
`

	var list AssetList
	if err := yaml.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if list.FindAsset("skill", "shared", "team") == nil {
		t.Fatal("应解析出 skill team/shared")
	}
	if list.FindAsset("rule", "shared", "team") == nil {
		t.Fatal("应解析出 rule team/shared")
	}
	if list.FindAsset("mcp", "postgres", "infra") == nil {
		t.Fatal("应解析出 mcp infra/postgres")
	}
	if list.Count() != 3 {
		t.Fatalf("解析后应有 3 个资产, 得到 %d", list.Count())
	}
}

func TestAssetListUnmarshalYAML_RejectsUnknownTypeKey(t *testing.T) {
	raw := `
team:
  broken:
    unknown: true
`

	var list AssetList
	err := yaml.Unmarshal([]byte(raw), &list)
	if err == nil {
		t.Fatal("应拒绝未知类型键")
	}
	if !strings.Contains(err.Error(), "不支持的类型键") {
		t.Fatalf("错误信息应包含不支持的类型键, 实际: %v", err)
	}
}
