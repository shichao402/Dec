package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

func TestInitProjectCreatesConfigFiles(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	if err := mgr.InitProject([]string{"cursor"}); err != nil {
		t.Fatalf("初始化项目失败: %v", err)
	}

	// 验证 ides.yaml 已创建
	idesPath := filepath.Join(projectRoot, ".dec", "config", "ides.yaml")
	if _, err := os.Stat(idesPath); err != nil {
		t.Fatalf("ides.yaml 应已创建: %v", err)
	}

	// 验证 vault.yaml 已创建
	vaultPath := filepath.Join(projectRoot, ".dec", "config", "vault.yaml")
	if _, err := os.Stat(vaultPath); err != nil {
		t.Fatalf("vault.yaml 应已创建: %v", err)
	}
}

func TestInitProjectRejectsInvalidIDE(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	err := mgr.InitProject([]string{"invalid-ide"})
	if err == nil {
		t.Fatalf("无效 IDE 应返回错误")
	}
	if !strings.Contains(err.Error(), "不支持的 IDE") {
		t.Fatalf("错误信息应包含 '不支持的 IDE'，得到: %s", err.Error())
	}
}

func TestCreateIDEsConfigEnabledAndCommented(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	if err := mgr.createIDEsConfig([]string{"cursor", "windsurf"}); err != nil {
		t.Fatalf("创建 IDE 配置失败: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, ".dec", "config", "ides.yaml"))
	if err != nil {
		t.Fatalf("读取 ides.yaml 失败: %v", err)
	}
	content := string(data)

	// cursor 和 windsurf 应启用
	if !strings.Contains(content, "  - cursor\n") {
		t.Fatalf("cursor 应启用（未注释）")
	}
	if !strings.Contains(content, "  - windsurf\n") {
		t.Fatalf("windsurf 应启用（未注释）")
	}
	// codebuddy 和 trae 应注释
	if !strings.Contains(content, "  # - codebuddy\n") {
		t.Fatalf("codebuddy 应被注释")
	}
	if !strings.Contains(content, "  # - trae\n") {
		t.Fatalf("trae 应被注释")
	}
}

func TestCreateIDEsConfigDefaultsCursor(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	// 空列表应默认启用 cursor
	if err := mgr.createIDEsConfig([]string{}); err != nil {
		t.Fatalf("创建 IDE 配置失败: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, ".dec", "config", "ides.yaml"))
	if err != nil {
		t.Fatalf("读取 ides.yaml 失败: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "  - cursor\n") {
		t.Fatalf("空列表时 cursor 应默认启用")
	}
}

func TestLoadIDEsConfigFileNotExist(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	config, err := mgr.LoadIDEsConfig()
	if err != nil {
		t.Fatalf("文件不存在时不应报错: %v", err)
	}
	if len(config.IDEs) != 1 || config.IDEs[0] != "cursor" {
		t.Fatalf("文件不存在时应默认返回 cursor，得到 %v", config.IDEs)
	}
}

func TestLoadVaultConfigFileNotExist(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	config, err := mgr.LoadVaultConfig()
	if err != nil {
		t.Fatalf("文件不存在时不应报错: %v", err)
	}
	if config.VaultSkills != nil || config.VaultRules != nil || config.VaultMCPs != nil {
		t.Fatalf("文件不存在时应返回空配置")
	}
}

func TestSaveAndLoadVaultConfig(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	// 先初始化目录
	if err := os.MkdirAll(filepath.Join(projectRoot, ".dec", "config"), 0755); err != nil {
		t.Fatal(err)
	}

	original := &VaultConfigV2{
		VaultSkills: []string{"skill-a", "skill-b"},
		VaultRules:  []string{"rule-x"},
		VaultMCPs:   []string{"mcp-1"},
	}

	if err := mgr.SaveVaultConfig(original); err != nil {
		t.Fatalf("保存 Vault 配置失败: %v", err)
	}

	loaded, err := mgr.LoadVaultConfig()
	if err != nil {
		t.Fatalf("加载 Vault 配置失败: %v", err)
	}

	if len(loaded.VaultSkills) != 2 || loaded.VaultSkills[0] != "skill-a" {
		t.Fatalf("Vault skills 不匹配: 得到 %v", loaded.VaultSkills)
	}
	if len(loaded.VaultRules) != 1 || loaded.VaultRules[0] != "rule-x" {
		t.Fatalf("Vault rules 不匹配: 得到 %v", loaded.VaultRules)
	}
	if len(loaded.VaultMCPs) != 1 || loaded.VaultMCPs[0] != "mcp-1" {
		t.Fatalf("Vault mcps 不匹配: 得到 %v", loaded.VaultMCPs)
	}
}

func TestEnsureVaultItem(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	if err := os.MkdirAll(filepath.Join(projectRoot, ".dec", "config"), 0755); err != nil {
		t.Fatal(err)
	}

	// 初始化空配置
	if err := mgr.SaveVaultConfig(&VaultConfigV2{}); err != nil {
		t.Fatal(err)
	}

	// 添加各类资产
	if err := mgr.EnsureVaultItem("skill", "test-skill"); err != nil {
		t.Fatalf("添加 skill 失败: %v", err)
	}
	if err := mgr.EnsureVaultItem("rule", "test-rule"); err != nil {
		t.Fatalf("添加 rule 失败: %v", err)
	}
	if err := mgr.EnsureVaultItem("mcp", "test-mcp"); err != nil {
		t.Fatalf("添加 mcp 失败: %v", err)
	}

	config, err := mgr.LoadVaultConfig()
	if err != nil {
		t.Fatal(err)
	}

	if len(config.VaultSkills) != 1 || config.VaultSkills[0] != "test-skill" {
		t.Fatalf("期望 skills=[test-skill]，得到 %v", config.VaultSkills)
	}
	if len(config.VaultRules) != 1 || config.VaultRules[0] != "test-rule" {
		t.Fatalf("期望 rules=[test-rule]，得到 %v", config.VaultRules)
	}
	if len(config.VaultMCPs) != 1 || config.VaultMCPs[0] != "test-mcp" {
		t.Fatalf("期望 mcps=[test-mcp]，得到 %v", config.VaultMCPs)
	}
}

func TestEnsureVaultItemRejectsInvalidType(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	if err := os.MkdirAll(filepath.Join(projectRoot, ".dec", "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveVaultConfig(&VaultConfigV2{}); err != nil {
		t.Fatal(err)
	}

	if err := mgr.EnsureVaultItem("invalid", "test"); err == nil {
		t.Fatalf("无效类型应返回错误")
	}
}

func TestEnsureVaultItemDeduplication(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	if err := os.MkdirAll(filepath.Join(projectRoot, ".dec", "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveVaultConfig(&VaultConfigV2{}); err != nil {
		t.Fatal(err)
	}

	// 重复添加同一个资产
	for i := 0; i < 3; i++ {
		if err := mgr.EnsureVaultItem("skill", "my-skill"); err != nil {
			t.Fatal(err)
		}
	}

	config, err := mgr.LoadVaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.VaultSkills) != 1 {
		t.Fatalf("重复添加应去重，期望 1 个，得到 %d 个", len(config.VaultSkills))
	}
}

func TestNormalizeStringList(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{"nil input", nil, nil},
		{"empty input", []string{}, nil},
		{"whitespace only", []string{"  ", "\t"}, nil},
		{"dedup", []string{"a", "b", "a"}, []string{"a", "b"}},
		{"trim", []string{"  hello  ", "world"}, []string{"hello", "world"}},
	}

	for _, tt := range tests {
		result := normalizeStringList(tt.input)
		if len(result) != len(tt.expected) {
			t.Fatalf("%s: 期望 %v，得到 %v", tt.name, tt.expected, result)
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Fatalf("%s: 期望 %v，得到 %v", tt.name, tt.expected, result)
			}
		}
	}
}

func TestNormalizeVaultConfigNil(t *testing.T) {
	result := normalizeVaultConfig(nil)
	if result == nil {
		t.Fatalf("nil 输入应返回空 config 而非 nil")
	}
	if result.VaultSkills != nil || result.VaultRules != nil || result.VaultMCPs != nil {
		t.Fatalf("nil 输入应返回全空配置")
	}
}

func TestRenderVaultConfigEmpty(t *testing.T) {
	content := renderVaultConfig(&VaultConfigV2{})

	// 应包含示例注释
	if !strings.Contains(content, "# - create-api-test") {
		t.Fatalf("空配置应渲染示例注释 create-api-test")
	}
	if !strings.Contains(content, "# - my-security-rule") {
		t.Fatalf("空配置应渲染示例注释 my-security-rule")
	}
	if !strings.Contains(content, "# - my-database-mcp") {
		t.Fatalf("空配置应渲染示例注释 my-database-mcp")
	}
}

func TestRenderVaultConfigWithValues(t *testing.T) {
	config := &VaultConfigV2{
		VaultSkills: []string{"real-skill"},
	}
	content := renderVaultConfig(config)

	if !strings.Contains(content, "  - real-skill\n") {
		t.Fatalf("有值时应渲染实际值")
	}
	// 有值的 section 不应显示示例注释
	if strings.Contains(content, "# - create-api-test") {
		t.Fatalf("有 skills 值时不应显示示例注释")
	}
	// 其他 section 仍应显示示例注释
	if !strings.Contains(content, "# - my-security-rule") {
		t.Fatalf("rules 为空时仍应显示示例注释")
	}
}

func TestExistsReturnsFalseWhenNotInitialized(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	if mgr.Exists() {
		t.Fatalf("未初始化时 Exists 应返回 false")
	}
}

func TestExistsReturnsTrueWhenInitialized(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManagerV2(projectRoot)

	if err := mgr.InitProject([]string{"cursor"}); err != nil {
		t.Fatal(err)
	}

	if !mgr.Exists() {
		t.Fatalf("初始化后 Exists 应返回 true")
	}
}

// VaultConfigV2 的别名，用于在 config 包内直接引用
type VaultConfigV2 = types.VaultConfigV2
