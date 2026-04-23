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

func TestProjectConfig_EnabledBundlesRoundTrip(t *testing.T) {
	cfg := &ProjectConfig{
		Version:        ProjectConfigVersionV2,
		EnabledBundles: []string{"vikunja", "helloworld"},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "enabled_bundles:") {
		t.Fatalf("序列化结果应包含 enabled_bundles:\n%s", content)
	}
	if !strings.Contains(content, "- vikunja") || !strings.Contains(content, "- helloworld") {
		t.Fatalf("序列化结果缺少 bundle 名:\n%s", content)
	}

	var parsed ProjectConfig
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if len(parsed.EnabledBundles) != 2 {
		t.Fatalf("反序列化后应有 2 个 bundle 引用, 得到 %d", len(parsed.EnabledBundles))
	}
	if parsed.EnabledBundles[0] != "vikunja" || parsed.EnabledBundles[1] != "helloworld" {
		t.Fatalf("bundle 引用顺序错误: %v", parsed.EnabledBundles)
	}
}

func TestProjectConfig_EnabledBundlesOmittedWhenEmpty(t *testing.T) {
	cfg := &ProjectConfig{Version: ProjectConfigVersionV2}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	if strings.Contains(string(data), "enabled_bundles") {
		t.Fatalf("空 EnabledBundles 不应出现在 YAML 中:\n%s", string(data))
	}
}

func TestProjectConfig_LegacyYAMLWithoutEnabledBundles(t *testing.T) {
	// 存量 config（没有 enabled_bundles 字段）应能正常解析，向后兼容
	raw := `
version: v2
ides:
  - cursor
available:
  cli:
    helloworld:
      skills: true
enabled:
  cli:
    helloworld:
      skills: true
`
	var cfg ProjectConfig
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("存量 config 解析失败: %v", err)
	}
	if len(cfg.EnabledBundles) != 0 {
		t.Fatalf("存量 config 的 EnabledBundles 应为空, 得到 %v", cfg.EnabledBundles)
	}
	if cfg.Enabled == nil || cfg.Enabled.FindAsset("skill", "helloworld", "cli") == nil {
		t.Fatalf("存量 enabled 字段应仍能解析")
	}
}
