package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/types"
)

type failingMCPIDE struct {
	name string
}

func (f *failingMCPIDE) Name() string {
	return f.name
}

func (f *failingMCPIDE) RulesDir(projectRoot string) string {
	return filepath.Join(projectRoot, "."+f.name, "rules")
}

func (f *failingMCPIDE) SkillsDir(projectRoot string) string {
	return filepath.Join(projectRoot, "."+f.name, "skills")
}

func (f *failingMCPIDE) MCPConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, "."+f.name, "mcp.json")
}

func (f *failingMCPIDE) WriteRules(projectRoot string, rules []ide.RuleFile) error {
	return nil
}

func (f *failingMCPIDE) WriteSkill(projectRoot string, skillName string, files []ide.SkillFile) error {
	return nil
}

func (f *failingMCPIDE) WriteMCPConfig(projectRoot string, config *types.MCPConfig) error {
	return nil
}

func (f *failingMCPIDE) LoadMCPConfig(projectRoot string) (*types.MCPConfig, error) {
	return nil, fmt.Errorf("mock MCP load failure")
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取当前目录失败: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("切换目录失败: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
}

func setEnvForTest(t *testing.T, key, value string) {
	t.Helper()

	oldValue, existed := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("设置环境变量失败: %v", err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, oldValue)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// ========================================
// isValidAssetType
// ========================================

func TestIsValidAssetType(t *testing.T) {
	valid := []string{"skill", "rule", "mcp"}
	for _, v := range valid {
		if !isValidAssetType(v) {
			t.Fatalf("期望 %q 是合法类型", v)
		}
	}

	invalid := []string{"", "skills", "rules", "mcps", "unknown", "Skill"}
	for _, v := range invalid {
		if isValidAssetType(v) {
			t.Fatalf("期望 %q 不是合法类型", v)
		}
	}
}

// ========================================
// managedName
// ========================================

func TestManagedName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-skill", "dec-my-skill"},
		{"dec-my-skill", "dec-my-skill"}, // 不重复添加
		{"", "dec-"},
		{"dec-", "dec-"},
	}
	for _, tt := range tests {
		got := managedName(tt.input)
		if got != tt.want {
			t.Fatalf("managedName(%q) = %q, 期望 %q", tt.input, got, tt.want)
		}
	}
}

// ========================================
// resolveTargetVault
// ========================================

func TestResolveTargetVault_Explicit(t *testing.T) {
	pc := &types.ProjectConfig{Vaults: []string{"v1", "v2"}}
	got, err := resolveTargetVault(pc, "explicit-vault")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if got != "explicit-vault" {
		t.Fatalf("期望 explicit-vault, 得到 %s", got)
	}
}

func TestResolveTargetVault_SingleVault(t *testing.T) {
	pc := &types.ProjectConfig{Vaults: []string{"only-vault"}}
	got, err := resolveTargetVault(pc, "")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if got != "only-vault" {
		t.Fatalf("期望 only-vault, 得到 %s", got)
	}
}

func TestResolveTargetVault_MultipleVaultsNoExplicit(t *testing.T) {
	pc := &types.ProjectConfig{Vaults: []string{"v1", "v2"}}
	_, err := resolveTargetVault(pc, "")
	if err == nil {
		t.Fatalf("多个 Vault 未指定 --vault 应返回错误")
	}
}

func TestResolveTargetVault_NoVaults(t *testing.T) {
	pc := &types.ProjectConfig{Vaults: nil}
	_, err := resolveTargetVault(pc, "")
	if err == nil {
		t.Fatalf("没有 Vault 应返回错误")
	}
}

// ========================================
// getAssetPath
// ========================================

func TestGetAssetPath(t *testing.T) {
	repoDir := "/fake/repo"

	tests := []struct {
		itemType  string
		assetName string
		want      string
	}{
		{"skill", "my-skill", filepath.Join(repoDir, "v1", "skills", "my-skill")},
		{"rule", "my-rule", filepath.Join(repoDir, "v1", "rules", "my-rule.mdc")},
		{"mcp", "my-mcp", filepath.Join(repoDir, "v1", "mcp", "my-mcp.json")},
		{"unknown", "x", ""},
	}
	for _, tt := range tests {
		got := getAssetPath(repoDir, "v1", tt.itemType, tt.assetName)
		if got != tt.want {
			t.Fatalf("getAssetPath(%q, %q) = %q, 期望 %q", tt.itemType, tt.assetName, got, tt.want)
		}
	}
}

// ========================================
// listVaultAssets
// ========================================

func TestListVaultAssets(t *testing.T) {
	vaultDir := t.TempDir()

	// 创建 vault 目录结构
	os.MkdirAll(filepath.Join(vaultDir, "skills", "api-test"), 0755)
	os.WriteFile(filepath.Join(vaultDir, "skills", "api-test", "SKILL.md"), []byte("test"), 0644)
	os.MkdirAll(filepath.Join(vaultDir, "rules"), 0755)
	os.WriteFile(filepath.Join(vaultDir, "rules", "logging.mdc"), []byte("# logging"), 0644)
	os.WriteFile(filepath.Join(vaultDir, "rules", ".gitkeep"), []byte(""), 0644)
	os.MkdirAll(filepath.Join(vaultDir, "mcp"), 0755)
	os.WriteFile(filepath.Join(vaultDir, "mcp", "postgres.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(vaultDir, "mcp", ".gitkeep"), []byte(""), 0644)

	assets := listVaultAssets(vaultDir, "test-vault")

	if len(assets) != 3 {
		t.Fatalf("期望 3 个资产, 得到 %d: %+v", len(assets), assets)
	}

	// 检查 .gitkeep 被跳过
	for _, a := range assets {
		if a.Name == ".gitkeep" {
			t.Fatalf(".gitkeep 不应出现在列表中")
		}
	}

	// 验证类型和名称
	found := map[string]string{}
	for _, a := range assets {
		found[a.Name] = a.Type
	}
	if found["api-test"] != "skill" {
		t.Fatalf("期望 api-test 类型为 skill, 得到 %s", found["api-test"])
	}
	if found["logging"] != "rule" {
		t.Fatalf("期望 logging 类型为 rule, 得到 %s", found["logging"])
	}
	if found["postgres"] != "mcp" {
		t.Fatalf("期望 postgres 类型为 mcp, 得到 %s", found["postgres"])
	}
}

func TestListVaultAssets_EmptyVault(t *testing.T) {
	vaultDir := t.TempDir()
	for _, sub := range []string{"skills", "rules", "mcp"} {
		os.MkdirAll(filepath.Join(vaultDir, sub), 0755)
		os.WriteFile(filepath.Join(vaultDir, sub, ".gitkeep"), []byte(""), 0644)
	}

	assets := listVaultAssets(vaultDir, "empty")
	if len(assets) != 0 {
		t.Fatalf("空 vault 应返回 0 个资产, 得到 %d", len(assets))
	}
}

func TestListVaultAssets_MissingSubDirs(t *testing.T) {
	vaultDir := t.TempDir()
	// 不创建任何子目录
	assets := listVaultAssets(vaultDir, "missing")
	if len(assets) != 0 {
		t.Fatalf("缺少子目录应返回 0 个资产, 得到 %d", len(assets))
	}
}

// ========================================
// collectAllAssets
// ========================================

func TestCollectAllAssets(t *testing.T) {
	ac := &types.AssetsConfig{
		Skills: []types.AssetEntry{
			{Name: "s1", Vault: "v1"},
			{Name: "s2", Vault: "v2"},
		},
		Rules: []types.AssetEntry{
			{Name: "r1", Vault: "v1"},
		},
		MCPs: []types.AssetEntry{
			{Name: "m1", Vault: "v1"},
		},
	}

	all := collectAllAssets(ac)
	if len(all) != 4 {
		t.Fatalf("期望 4 个资产, 得到 %d", len(all))
	}

	// 验证类型分布
	typeCounts := map[string]int{}
	for _, a := range all {
		typeCounts[a.Type]++
	}
	if typeCounts["skill"] != 2 {
		t.Fatalf("期望 2 个 skill, 得到 %d", typeCounts["skill"])
	}
	if typeCounts["rule"] != 1 {
		t.Fatalf("期望 1 个 rule, 得到 %d", typeCounts["rule"])
	}
	if typeCounts["mcp"] != 1 {
		t.Fatalf("期望 1 个 mcp, 得到 %d", typeCounts["mcp"])
	}
}

func TestCollectAllAssets_Empty(t *testing.T) {
	ac := &types.AssetsConfig{}
	all := collectAllAssets(ac)
	if len(all) != 0 {
		t.Fatalf("空配置应返回 0 个资产, 得到 %d", len(all))
	}
}

// ========================================
// getLocalAssetPath
// ========================================

func TestGetLocalAssetPath(t *testing.T) {
	projectRoot := "/fake/project"
	cursorIDE := ide.Get("cursor")

	tests := []struct {
		itemType string
		name     string
		want     string
	}{
		{"skill", "my-skill", filepath.Join(projectRoot, ".cursor", "skills", "dec-my-skill")},
		{"rule", "my-rule", filepath.Join(projectRoot, ".cursor", "rules", "dec-my-rule.mdc")},
		{"mcp", "my-mcp", filepath.Join(projectRoot, ".cursor", "mcp.json")},
		{"unknown", "x", ""},
	}
	for _, tt := range tests {
		got := getLocalAssetPath(tt.itemType, tt.name, projectRoot, cursorIDE)
		if got != tt.want {
			t.Fatalf("getLocalAssetPath(%q, %q) = %q, 期望 %q", tt.itemType, tt.name, got, tt.want)
		}
	}
}

func TestGetLocalAssetPath_CodeBuddy(t *testing.T) {
	projectRoot := "/fake/project"
	codebuddyIDE := ide.Get("codebuddy")

	// CodeBuddy MCP 路径特殊：项目根 .mcp.json
	got := getLocalAssetPath("mcp", "pg", projectRoot, codebuddyIDE)
	want := filepath.Join(projectRoot, ".mcp.json")
	if got != want {
		t.Fatalf("CodeBuddy MCP 路径应为 %q, 得到 %q", want, got)
	}

	// CodeBuddy skill 路径
	got = getLocalAssetPath("skill", "my-skill", projectRoot, codebuddyIDE)
	want = filepath.Join(projectRoot, ".codebuddy", "skills", "dec-my-skill")
	if got != want {
		t.Fatalf("CodeBuddy skill 路径应为 %q, 得到 %q", want, got)
	}
}

// ========================================
// saveSkillToVault
// ========================================

func TestSaveSkillToVault(t *testing.T) {
	sourceDir := t.TempDir()
	skillDir := filepath.Join(sourceDir, "my-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\n---\n"), 0644)
	os.WriteFile(filepath.Join(skillDir, "helper.py"), []byte("print('hello')"), 0644)

	vaultDir := t.TempDir()
	os.MkdirAll(filepath.Join(vaultDir, "skills"), 0755)

	info, _ := os.Stat(skillDir)
	name, err := saveSkillToVault(skillDir, info, vaultDir)
	if err != nil {
		t.Fatalf("保存 skill 失败: %v", err)
	}
	if name != "my-skill" {
		t.Fatalf("期望名称 my-skill, 得到 %s", name)
	}

	// 验证文件已复制
	if _, err := os.Stat(filepath.Join(vaultDir, "skills", "my-skill", "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md 未复制到 vault: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "skills", "my-skill", "helper.py")); err != nil {
		t.Fatalf("helper.py 未复制到 vault: %v", err)
	}
}

func TestSaveSkillToVault_StripDecPrefix(t *testing.T) {
	sourceDir := t.TempDir()
	skillDir := filepath.Join(sourceDir, "dec-my-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("test"), 0644)

	vaultDir := t.TempDir()
	os.MkdirAll(filepath.Join(vaultDir, "skills"), 0755)

	info, _ := os.Stat(skillDir)
	name, err := saveSkillToVault(skillDir, info, vaultDir)
	if err != nil {
		t.Fatalf("保存 skill 失败: %v", err)
	}
	if name != "my-skill" {
		t.Fatalf("应去掉 dec- 前缀, 期望 my-skill, 得到 %s", name)
	}
}

func TestSaveSkillToVault_NotDir(t *testing.T) {
	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "not-a-dir.md")
	os.WriteFile(filePath, []byte("test"), 0644)

	vaultDir := t.TempDir()

	info, _ := os.Stat(filePath)
	_, err := saveSkillToVault(filePath, info, vaultDir)
	if err == nil {
		t.Fatalf("非目录应返回错误")
	}
}

func TestSaveSkillToVault_MissingSkillMD(t *testing.T) {
	sourceDir := t.TempDir()
	skillDir := filepath.Join(sourceDir, "bad-skill")
	os.MkdirAll(skillDir, 0755)
	// 不创建 SKILL.md

	vaultDir := t.TempDir()

	info, _ := os.Stat(skillDir)
	_, err := saveSkillToVault(skillDir, info, vaultDir)
	if err == nil {
		t.Fatalf("缺少 SKILL.md 应返回错误")
	}
}

// ========================================
// saveRuleToVault
// ========================================

func TestSaveRuleToVault(t *testing.T) {
	sourceDir := t.TempDir()
	rulePath := filepath.Join(sourceDir, "logging.mdc")
	os.WriteFile(rulePath, []byte("# Logging Rule\ncontent here"), 0644)

	vaultDir := t.TempDir()
	os.MkdirAll(filepath.Join(vaultDir, "rules"), 0755)

	name, err := saveRuleToVault(rulePath, vaultDir)
	if err != nil {
		t.Fatalf("保存 rule 失败: %v", err)
	}
	if name != "logging" {
		t.Fatalf("期望名称 logging, 得到 %s", name)
	}

	// 验证文件已复制
	data, err := os.ReadFile(filepath.Join(vaultDir, "rules", "logging.mdc"))
	if err != nil {
		t.Fatalf("rule 文件未复制到 vault: %v", err)
	}
	if string(data) != "# Logging Rule\ncontent here" {
		t.Fatalf("rule 内容不匹配")
	}
}

func TestSaveRuleToVault_StripDecPrefix(t *testing.T) {
	sourceDir := t.TempDir()
	rulePath := filepath.Join(sourceDir, "dec-my-rule.mdc")
	os.WriteFile(rulePath, []byte("test"), 0644)

	vaultDir := t.TempDir()
	os.MkdirAll(filepath.Join(vaultDir, "rules"), 0755)

	name, err := saveRuleToVault(rulePath, vaultDir)
	if err != nil {
		t.Fatalf("保存 rule 失败: %v", err)
	}
	if name != "my-rule" {
		t.Fatalf("应去掉 dec- 前缀, 期望 my-rule, 得到 %s", name)
	}
}

func TestSaveRuleToVault_WrongExtension(t *testing.T) {
	sourceDir := t.TempDir()
	rulePath := filepath.Join(sourceDir, "rule.txt")
	os.WriteFile(rulePath, []byte("test"), 0644)

	vaultDir := t.TempDir()

	_, err := saveRuleToVault(rulePath, vaultDir)
	if err == nil {
		t.Fatalf("非 .mdc 文件应返回错误")
	}
}

// ========================================
// saveMCPToVault
// ========================================

func TestSaveMCPToVault(t *testing.T) {
	sourceDir := t.TempDir()
	mcpPath := filepath.Join(sourceDir, "postgres.json")
	os.WriteFile(mcpPath, []byte(`{"command":"npx","args":["-y","pg-tool"]}`), 0644)

	vaultDir := t.TempDir()
	os.MkdirAll(filepath.Join(vaultDir, "mcp"), 0755)

	name, err := saveMCPToVault(mcpPath, vaultDir)
	if err != nil {
		t.Fatalf("保存 MCP 失败: %v", err)
	}
	if name != "postgres" {
		t.Fatalf("期望名称 postgres, 得到 %s", name)
	}

	// 验证文件已复制
	if _, err := os.Stat(filepath.Join(vaultDir, "mcp", "postgres.json")); err != nil {
		t.Fatalf("MCP 文件未复制到 vault: %v", err)
	}
}

func TestSaveMCPToVault_StripDecPrefix(t *testing.T) {
	sourceDir := t.TempDir()
	mcpPath := filepath.Join(sourceDir, "dec-pg.json")
	os.WriteFile(mcpPath, []byte(`{"command":"npx"}`), 0644)

	vaultDir := t.TempDir()
	os.MkdirAll(filepath.Join(vaultDir, "mcp"), 0755)

	name, err := saveMCPToVault(mcpPath, vaultDir)
	if err != nil {
		t.Fatalf("保存 MCP 失败: %v", err)
	}
	if name != "pg" {
		t.Fatalf("应去掉 dec- 前缀, 期望 pg, 得到 %s", name)
	}
}

func TestSaveMCPToVault_WrongExtension(t *testing.T) {
	sourceDir := t.TempDir()
	mcpPath := filepath.Join(sourceDir, "mcp.yaml")
	os.WriteFile(mcpPath, []byte("test"), 0644)

	vaultDir := t.TempDir()
	_, err := saveMCPToVault(mcpPath, vaultDir)
	if err == nil {
		t.Fatalf("非 .json 文件应返回错误")
	}
}

func TestSaveMCPToVault_InvalidJSON(t *testing.T) {
	sourceDir := t.TempDir()
	mcpPath := filepath.Join(sourceDir, "bad.json")
	os.WriteFile(mcpPath, []byte("not json"), 0644)

	vaultDir := t.TempDir()
	os.MkdirAll(filepath.Join(vaultDir, "mcp"), 0755)

	_, err := saveMCPToVault(mcpPath, vaultDir)
	if err == nil {
		t.Fatalf("无效 JSON 应返回错误")
	}
}

func TestSaveMCPToVault_MissingCommand(t *testing.T) {
	sourceDir := t.TempDir()
	mcpPath := filepath.Join(sourceDir, "no-cmd.json")
	os.WriteFile(mcpPath, []byte(`{"args":["foo"]}`), 0644)

	vaultDir := t.TempDir()
	os.MkdirAll(filepath.Join(vaultDir, "mcp"), 0755)

	_, err := saveMCPToVault(mcpPath, vaultDir)
	if err == nil {
		t.Fatalf("缺少 command 字段应返回错误")
	}
}

// ========================================
// findAssetInVaults
// ========================================

func TestFindAssetInVaults_InAssociatedVault(t *testing.T) {
	repoDir := t.TempDir()

	// 创建 vault 并放入 skill
	skillDir := filepath.Join(repoDir, "v1", "skills", "api-test")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("test"), 0644)

	foundVault, foundPath, err := findAssetInVaults(repoDir, []string{"v1"}, "skill", "api-test")
	if err != nil {
		t.Fatalf("查找失败: %v", err)
	}
	if foundVault != "v1" {
		t.Fatalf("期望 v1, 得到 %s", foundVault)
	}
	if foundPath != filepath.Join(repoDir, "v1", "skills", "api-test") {
		t.Fatalf("路径不匹配: %s", foundPath)
	}
}

func TestFindAssetInVaults_FallbackToAllVaults(t *testing.T) {
	repoDir := t.TempDir()

	// 在 v2（未关联）中放入资产
	os.MkdirAll(filepath.Join(repoDir, "v1", "skills"), 0755)
	ruleDir := filepath.Join(repoDir, "v2", "rules")
	os.MkdirAll(ruleDir, 0755)
	os.WriteFile(filepath.Join(ruleDir, "logging.mdc"), []byte("test"), 0644)

	// 关联 v1，但资产在 v2 中
	foundVault, _, err := findAssetInVaults(repoDir, []string{"v1"}, "rule", "logging")
	if err != nil {
		t.Fatalf("查找失败: %v", err)
	}
	if foundVault != "v2" {
		t.Fatalf("应回退到全扫描找到 v2, 得到 %s", foundVault)
	}
}

func TestFindAssetInVaults_NotFound(t *testing.T) {
	repoDir := t.TempDir()
	os.MkdirAll(filepath.Join(repoDir, "v1", "skills"), 0755)

	_, _, err := findAssetInVaults(repoDir, []string{"v1"}, "skill", "nonexistent")
	if err == nil {
		t.Fatalf("不存在的资产应返回错误")
	}
}

func TestFindAssetInVaults_SkipsHiddenDirs(t *testing.T) {
	repoDir := t.TempDir()

	// 在隐藏目录中放入资产（不应被发现）
	os.MkdirAll(filepath.Join(repoDir, ".git", "skills", "hidden-skill"), 0755)

	_, _, err := findAssetInVaults(repoDir, nil, "skill", "hidden-skill")
	if err == nil {
		t.Fatalf("隐藏目录中的资产不应被找到")
	}
}

func TestFindAssetInVaults_MCPType(t *testing.T) {
	repoDir := t.TempDir()
	os.MkdirAll(filepath.Join(repoDir, "v1", "mcp"), 0755)
	os.WriteFile(filepath.Join(repoDir, "v1", "mcp", "pg-tool.json"), []byte(`{"command":"npx"}`), 0644)

	foundVault, _, err := findAssetInVaults(repoDir, []string{"v1"}, "mcp", "pg-tool")
	if err != nil {
		t.Fatalf("查找 MCP 失败: %v", err)
	}
	if foundVault != "v1" {
		t.Fatalf("期望 v1, 得到 %s", foundVault)
	}
}

// ========================================
// installAssetToIDE / removeAssetFromIDE
// ========================================

func TestInstallSkillToIDE(t *testing.T) {
	projectRoot := t.TempDir()

	// 准备 vault 中的 skill 源
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: test\n---"), 0644)
	os.WriteFile(filepath.Join(srcDir, "helper.py"), []byte("pass"), 0644)

	cursorIDE := ide.Get("cursor")
	err := installAssetToIDE("skill", "my-skill", srcDir, projectRoot, cursorIDE)
	if err != nil {
		t.Fatalf("安装 skill 失败: %v", err)
	}

	// 验证使用 dec- 前缀
	destDir := filepath.Join(projectRoot, ".cursor", "skills", "dec-my-skill")
	if _, err := os.Stat(filepath.Join(destDir, "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md 未安装: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "helper.py")); err != nil {
		t.Fatalf("helper.py 未安装: %v", err)
	}

	// 不应创建不带前缀的目录
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "my-skill")); !os.IsNotExist(err) {
		t.Fatalf("不应创建不带 dec- 前缀的目录")
	}
}

func TestInstallRuleToIDE(t *testing.T) {
	projectRoot := t.TempDir()

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "logging.mdc")
	os.WriteFile(srcPath, []byte("# logging rule"), 0644)

	cursorIDE := ide.Get("cursor")
	err := installAssetToIDE("rule", "logging", srcPath, projectRoot, cursorIDE)
	if err != nil {
		t.Fatalf("安装 rule 失败: %v", err)
	}

	destPath := filepath.Join(projectRoot, ".cursor", "rules", "dec-logging.mdc")
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("rule 未安装: %v", err)
	}
	if string(data) != "# logging rule" {
		t.Fatalf("rule 内容不匹配")
	}
}

func TestInstallMCPToIDE(t *testing.T) {
	projectRoot := t.TempDir()

	// 创建已有的 MCP 配置
	existingConfig := types.MCPConfig{
		MCPServers: map[string]types.MCPServer{
			"user-tool": {Command: "npx", Args: []string{"-y", "user-tool"}},
		},
	}
	mcpDir := filepath.Join(projectRoot, ".cursor")
	os.MkdirAll(mcpDir, 0755)
	data, _ := json.Marshal(existingConfig)
	os.WriteFile(filepath.Join(mcpDir, "mcp.json"), data, 0644)

	// 准备 MCP 源文件
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "pg-tool.json")
	os.WriteFile(srcPath, []byte(`{"command":"npx","args":["-y","pg-tool"]}`), 0644)

	cursorIDE := ide.Get("cursor")
	err := installAssetToIDE("mcp", "pg-tool", srcPath, projectRoot, cursorIDE)
	if err != nil {
		t.Fatalf("安装 MCP 失败: %v", err)
	}

	// 验证合并结果
	resultData, err := os.ReadFile(filepath.Join(mcpDir, "mcp.json"))
	if err != nil {
		t.Fatalf("读取 MCP 配置失败: %v", err)
	}
	var result types.MCPConfig
	if err := json.Unmarshal(resultData, &result); err != nil {
		t.Fatalf("解析 MCP 配置失败: %v", err)
	}

	// 用户的 MCP 条目保留
	if _, ok := result.MCPServers["user-tool"]; !ok {
		t.Fatalf("用户 MCP 条目被覆盖")
	}
	// 新条目使用 dec- 前缀
	if _, ok := result.MCPServers["dec-pg-tool"]; !ok {
		t.Fatalf("托管 MCP 条目未写入")
	}
}

func TestInstallMCPToIDE_NoExistingConfig(t *testing.T) {
	projectRoot := t.TempDir()

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "tool.json")
	os.WriteFile(srcPath, []byte(`{"command":"node","args":["server.js"]}`), 0644)

	cursorIDE := ide.Get("cursor")
	err := installAssetToIDE("mcp", "tool", srcPath, projectRoot, cursorIDE)
	if err != nil {
		t.Fatalf("安装 MCP 到空项目失败: %v", err)
	}

	resultData, err := os.ReadFile(filepath.Join(projectRoot, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatalf("MCP 配置未创建: %v", err)
	}
	var result types.MCPConfig
	json.Unmarshal(resultData, &result)
	if _, ok := result.MCPServers["dec-tool"]; !ok {
		t.Fatalf("托管 MCP 条目未写入")
	}
}

func TestRemoveSkillFromIDE(t *testing.T) {
	projectRoot := t.TempDir()

	// 先安装
	skillDir := filepath.Join(projectRoot, ".cursor", "skills", "dec-my-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("test"), 0644)

	cursorIDE := ide.Get("cursor")
	_, err := removeAssetFromIDE("skill", "my-skill", projectRoot, cursorIDE)
	if err != nil {
		t.Fatalf("移除 skill 失败: %v", err)
	}

	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatalf("skill 目录应被删除")
	}
}

func TestRemoveRuleFromIDE(t *testing.T) {
	projectRoot := t.TempDir()

	rulePath := filepath.Join(projectRoot, ".cursor", "rules", "dec-logging.mdc")
	os.MkdirAll(filepath.Dir(rulePath), 0755)
	os.WriteFile(rulePath, []byte("test"), 0644)

	cursorIDE := ide.Get("cursor")
	_, err := removeAssetFromIDE("rule", "logging", projectRoot, cursorIDE)
	if err != nil {
		t.Fatalf("移除 rule 失败: %v", err)
	}

	if _, err := os.Stat(rulePath); !os.IsNotExist(err) {
		t.Fatalf("rule 文件应被删除")
	}
}

func TestRemoveMCPFromIDE(t *testing.T) {
	projectRoot := t.TempDir()

	existingConfig := types.MCPConfig{
		MCPServers: map[string]types.MCPServer{
			"user-tool":   {Command: "npx"},
			"dec-pg-tool": {Command: "npx"},
		},
	}
	mcpDir := filepath.Join(projectRoot, ".cursor")
	os.MkdirAll(mcpDir, 0755)
	data, _ := json.Marshal(existingConfig)
	os.WriteFile(filepath.Join(mcpDir, "mcp.json"), data, 0644)

	cursorIDE := ide.Get("cursor")
	_, err := removeAssetFromIDE("mcp", "pg-tool", projectRoot, cursorIDE)
	if err != nil {
		t.Fatalf("移除 MCP 失败: %v", err)
	}

	resultData, _ := os.ReadFile(filepath.Join(mcpDir, "mcp.json"))
	var result types.MCPConfig
	json.Unmarshal(resultData, &result)

	// 用户条目保留
	if _, ok := result.MCPServers["user-tool"]; !ok {
		t.Fatalf("用户 MCP 不应被删除")
	}
	// 托管条目已删除
	if _, ok := result.MCPServers["dec-pg-tool"]; ok {
		t.Fatalf("托管 MCP 应被删除")
	}
}

func TestRemoveAssetFromIDE_NotExists(t *testing.T) {
	projectRoot := t.TempDir()

	cursorIDE := ide.Get("cursor")

	// 删除不存在的资产不应报错
	if _, err := removeAssetFromIDE("skill", "nonexistent", projectRoot, cursorIDE); err != nil {
		t.Fatalf("删除不存在的 skill 不应报错: %v", err)
	}
	if _, err := removeAssetFromIDE("rule", "nonexistent", projectRoot, cursorIDE); err != nil {
		t.Fatalf("删除不存在的 rule 不应报错: %v", err)
	}
}

// ========================================
// installAssetToIDE — 多 IDE
// ========================================

func TestInstallSkillToMultipleIDEs(t *testing.T) {
	projectRoot := t.TempDir()

	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("test"), 0644)

	for _, ideName := range []string{"cursor", "windsurf"} {
		ideImpl := ide.Get(ideName)
		if err := installAssetToIDE("skill", "cross-ide", srcDir, projectRoot, ideImpl); err != nil {
			t.Fatalf("安装到 %s 失败: %v", ideName, err)
		}
	}

	// 两个 IDE 目录都应有文件
	for _, dir := range []string{".cursor", ".windsurf"} {
		path := filepath.Join(projectRoot, dir, "skills", "dec-cross-ide", "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s 下 skill 未安装: %v", dir, err)
		}
	}
}

func TestPullSingleAsset_RollsBackInstalledIDEsOnFailure(t *testing.T) {
	decHome, repoDir := setupVaultRemoveRemoteTestRepo(t)
	projectRoot := t.TempDir()
	failingIDEName := "failing-mcp-rollback"

	setEnvForTest(t, "DEC_HOME", decHome)
	chdirForTest(t, projectRoot)
	ide.Register(&failingMCPIDE{name: failingIDEName})

	repoMCPDir := filepath.Join(repoDir, "v1", "mcp")
	if err := os.MkdirAll(repoMCPDir, 0755); err != nil {
		t.Fatalf("创建测试仓库失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoMCPDir, "pg-tool.json"), []byte(`{"command":"npx","args":["-y","pg-tool"]}`), 0644); err != nil {
		t.Fatalf("写入测试 MCP 失败: %v", err)
	}
	runVaultTestGit(t, repoDir, "add", ".")
	runVaultTestGit(t, repoDir, "commit", "-m", "seed rollback mcp")
	runVaultTestGit(t, repoDir, "push", "origin", "main")

	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.InitProject("v1", []string{"cursor", failingIDEName}); err != nil {
		t.Fatalf("初始化项目失败: %v", err)
	}

	err := pullSingleAsset("mcp", "pg-tool")
	if err == nil {
		t.Fatalf("期望安装失败并触发回滚")
	}
	if !strings.Contains(err.Error(), "安装到 failing-mcp-rollback 失败") {
		t.Fatalf("错误信息应包含失败 IDE, 得到: %v", err)
	}

	assetsConfig, err := mgr.LoadAssetsConfig()
	if err != nil {
		t.Fatalf("读取资产追踪失败: %v", err)
	}
	if asset := assetsConfig.FindAsset("mcp", "pg-tool"); asset != nil {
		t.Fatalf("回滚后不应写入资产追踪: %+v", *asset)
	}

	cursorConfigPath := filepath.Join(projectRoot, ".cursor", "mcp.json")
	data, err := os.ReadFile(cursorConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("读取 cursor MCP 配置失败: %v", err)
	}

	var cfg types.MCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("解析 cursor MCP 配置失败: %v", err)
	}
	if _, ok := cfg.MCPServers["dec-pg-tool"]; ok {
		t.Fatalf("回滚后 cursor 不应保留托管 MCP 条目")
	}
}

func TestRunVaultRemove_ReturnsAssetsLoadError(t *testing.T) {
	projectRoot := t.TempDir()
	chdirForTest(t, projectRoot)

	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{Vaults: []string{"v1"}, IDEs: []string{"cursor"}}); err != nil {
		t.Fatalf("写入项目配置失败: %v", err)
	}

	assetsPath := filepath.Join(mgr.GetDecDir(), "assets.yaml")
	if err := os.MkdirAll(assetsPath, 0755); err != nil {
		t.Fatalf("创建损坏的 assets.yaml 目录失败: %v", err)
	}

	oldRemoveRemote := removeRemote
	removeRemote = false
	t.Cleanup(func() {
		removeRemote = oldRemoveRemote
	})

	err := runVaultRemove(nil, []string{"skill", "missing"})
	if err == nil {
		t.Fatalf("期望返回资产追踪加载错误")
	}
	if !strings.Contains(err.Error(), "加载资产追踪失败") {
		t.Fatalf("错误信息不正确: %v", err)
	}
}

func TestRunVaultRemove_RemoteUsesTrackedVault(t *testing.T) {
	decHome, repoDir := setupVaultRemoveRemoteTestRepo(t)
	setEnvForTest(t, "DEC_HOME", decHome)

	writeVaultTestFile(t, filepath.Join(repoDir, "v1", "rules", "shared.mdc"), "from v1\n")
	writeVaultTestFile(t, filepath.Join(repoDir, "v2", "rules", "shared.mdc"), "from v2\n")
	runVaultTestGit(t, repoDir, "add", ".")
	runVaultTestGit(t, repoDir, "commit", "-m", "seed duplicate rules")
	runVaultTestGit(t, repoDir, "push", "origin", "main")

	projectRoot := t.TempDir()
	chdirForTest(t, projectRoot)
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{Vaults: []string{"v1", "v2"}, IDEs: []string{"cursor"}}); err != nil {
		t.Fatalf("写入项目配置失败: %v", err)
	}
	if err := mgr.SaveAssetsConfig(&types.AssetsConfig{Rules: []types.AssetEntry{{Name: "shared", Vault: "v1", InstalledAt: "2026-04-01T00:00:00Z"}}}); err != nil {
		t.Fatalf("写入资产追踪失败: %v", err)
	}

	oldRemoveRemote := removeRemote
	removeRemote = true
	t.Cleanup(func() {
		removeRemote = oldRemoveRemote
	})

	if err := runVaultRemove(nil, []string{"rule", "shared"}); err != nil {
		t.Fatalf("远程删除追踪资产失败: %v", err)
	}
	runVaultTestGit(t, repoDir, "pull", "--ff-only")

	if _, err := os.Stat(filepath.Join(repoDir, "v1", "rules", "shared.mdc")); !os.IsNotExist(err) {
		t.Fatalf("追踪来源 v1 中的远程资产应被删除")
	}
	if _, err := os.Stat(filepath.Join(repoDir, "v2", "rules", "shared.mdc")); err != nil {
		t.Fatalf("其他 vault 中的同名资产不应被删除: %v", err)
	}

	assetsConfig, err := mgr.LoadAssetsConfig()
	if err != nil {
		t.Fatalf("读取资产追踪失败: %v", err)
	}
	if asset := assetsConfig.FindAsset("rule", "shared"); asset != nil {
		t.Fatalf("移除后不应保留资产追踪: %+v", *asset)
	}
}

func TestRunVaultRemove_RemoteFallbackWhenNotTracked(t *testing.T) {
	decHome, repoDir := setupVaultRemoveRemoteTestRepo(t)
	setEnvForTest(t, "DEC_HOME", decHome)

	writeVaultTestFile(t, filepath.Join(repoDir, "v1", "rules", "shared.mdc"), "from v1\n")
	writeVaultTestFile(t, filepath.Join(repoDir, "v2", "rules", "shared.mdc"), "from v2\n")
	runVaultTestGit(t, repoDir, "add", ".")
	runVaultTestGit(t, repoDir, "commit", "-m", "seed duplicate fallback rules")
	runVaultTestGit(t, repoDir, "push", "origin", "main")

	projectRoot := t.TempDir()
	chdirForTest(t, projectRoot)
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{Vaults: []string{"v1"}, IDEs: []string{"cursor"}}); err != nil {
		t.Fatalf("写入项目配置失败: %v", err)
	}
	if err := mgr.SaveAssetsConfig(&types.AssetsConfig{}); err != nil {
		t.Fatalf("写入空资产追踪失败: %v", err)
	}

	oldRemoveRemote := removeRemote
	removeRemote = true
	t.Cleanup(func() {
		removeRemote = oldRemoveRemote
	})

	if err := runVaultRemove(nil, []string{"rule", "shared"}); err != nil {
		t.Fatalf("远程删除未追踪资产失败: %v", err)
	}
	runVaultTestGit(t, repoDir, "pull", "--ff-only")

	if _, err := os.Stat(filepath.Join(repoDir, "v1", "rules", "shared.mdc")); !os.IsNotExist(err) {
		t.Fatalf("fallback 应只删除关联 vault 中找到的首个匹配")
	}
	if _, err := os.Stat(filepath.Join(repoDir, "v2", "rules", "shared.mdc")); err != nil {
		t.Fatalf("未追踪 fallback 不应删除其他 vault 的同名资产: %v", err)
	}
}

func TestRunVaultRemove_RemoteNotFoundWhenTrackedButMissing(t *testing.T) {
	decHome, repoDir := setupVaultRemoveRemoteTestRepo(t)
	setEnvForTest(t, "DEC_HOME", decHome)

	writeVaultTestFile(t, filepath.Join(repoDir, "v2", "rules", "shared.mdc"), "from v2\n")
	runVaultTestGit(t, repoDir, "add", ".")
	runVaultTestGit(t, repoDir, "commit", "-m", "seed alternate vault rule")
	runVaultTestGit(t, repoDir, "push", "origin", "main")

	projectRoot := t.TempDir()
	chdirForTest(t, projectRoot)
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{Vaults: []string{"v1", "v2"}, IDEs: []string{"cursor"}}); err != nil {
		t.Fatalf("写入项目配置失败: %v", err)
	}
	if err := mgr.SaveAssetsConfig(&types.AssetsConfig{Rules: []types.AssetEntry{{Name: "shared", Vault: "v1", InstalledAt: "2026-04-01T00:00:00Z"}}}); err != nil {
		t.Fatalf("写入资产追踪失败: %v", err)
	}

	oldRemoveRemote := removeRemote
	removeRemote = true
	t.Cleanup(func() {
		removeRemote = oldRemoveRemote
	})

	if err := runVaultRemove(nil, []string{"rule", "shared"}); err != nil {
		t.Fatalf("追踪来源远程缺失时不应失败: %v", err)
	}
	runVaultTestGit(t, repoDir, "pull", "--ff-only")

	if _, err := os.Stat(filepath.Join(repoDir, "v2", "rules", "shared.mdc")); err != nil {
		t.Fatalf("追踪来源缺失时不应删除其他 vault 的同名资产: %v", err)
	}

	assetsConfig, err := mgr.LoadAssetsConfig()
	if err != nil {
		t.Fatalf("读取资产追踪失败: %v", err)
	}
	if asset := assetsConfig.FindAsset("rule", "shared"); asset != nil {
		t.Fatalf("移除后不应保留资产追踪: %+v", *asset)
	}
}

func TestBareWorkflow_EndToEnd(t *testing.T) {
	decHome := t.TempDir()
	root := t.TempDir()
	remoteBareDir := filepath.Join(root, "remote.git")
	seedDir := filepath.Join(root, "seed")
	inspectDir := filepath.Join(root, "inspect")

	runVaultTestGitNoDir(t, "init", "--bare", remoteBareDir)
	runVaultTestGitNoDir(t, "clone", remoteBareDir, seedDir)
	configureVaultTestGitUser(t, seedDir)
	writeVaultTestFile(t, filepath.Join(seedDir, "README.md"), "init\n")
	runVaultTestGit(t, seedDir, "add", ".")
	runVaultTestGit(t, seedDir, "commit", "-m", "initial commit")
	runVaultTestGit(t, seedDir, "branch", "-M", "main")
	runVaultTestGit(t, seedDir, "push", "-u", "origin", "main")
	runVaultTestGitNoDir(t, "--git-dir", remoteBareDir, "symbolic-ref", "HEAD", "refs/heads/main")
	runVaultTestGitNoDir(t, "clone", remoteBareDir, inspectDir)
	configureVaultTestGitUser(t, inspectDir)
	setEnvForTest(t, "DEC_HOME", decHome)

	projectOne := t.TempDir()
	chdirForTest(t, projectOne)

	if err := runRepo(nil, []string{remoteBareDir}); err != nil {
		t.Fatalf("runRepo 失败: %v", err)
	}
	bareDir := filepath.Join(decHome, "repo.git")
	if _, err := os.Stat(filepath.Join(bareDir, "HEAD")); err != nil {
		t.Fatalf("连接后应创建 bare repo: %v", err)
	}
	configureVaultTestBareGitUser(t, bareDir)

	if err := runVaultInit(nil, []string{"team-vault"}); err != nil {
		t.Fatalf("runVaultInit 失败: %v", err)
	}
	refreshInspectRepo(t, inspectDir)
	for _, sub := range []string{"skills", "rules", "mcp"} {
		if _, err := os.Stat(filepath.Join(inspectDir, "team-vault", sub, ".gitkeep")); err != nil {
			t.Fatalf("vault 初始化后缺少 %s/.gitkeep: %v", sub, err)
		}
	}

	skillDir := filepath.Join(projectOne, "my-skill")
	writeVaultTestFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: my-skill\n---\n")
	writeVaultTestFile(t, filepath.Join(skillDir, "helper.py"), "print('v1')\n")
	if err := runVaultImport(nil, []string{"skill", skillDir}); err != nil {
		t.Fatalf("runVaultImport 失败: %v", err)
	}
	refreshInspectRepo(t, inspectDir)
	if _, err := os.Stat(filepath.Join(inspectDir, "team-vault", "skills", "my-skill", "SKILL.md")); err != nil {
		t.Fatalf("保存后远端应存在 skill: %v", err)
	}

	projectTwo := t.TempDir()
	chdirForTest(t, projectTwo)
	if err := runVaultInit(nil, []string{"team-vault"}); err != nil {
		t.Fatalf("第二个项目 runVaultInit 失败: %v", err)
	}
	if err := runVaultPull(nil, []string{"skill", "my-skill"}); err != nil {
		t.Fatalf("runVaultPull 失败: %v", err)
	}

	localSkillDir := getLocalAssetPath("skill", "my-skill", projectTwo, ide.Get("cursor"))
	localHelper := filepath.Join(localSkillDir, "helper.py")
	if data, err := os.ReadFile(localHelper); err != nil {
		t.Fatalf("拉取后本地应存在 helper.py: %v", err)
	} else if !strings.Contains(string(data), "v1") {
		t.Fatalf("拉取后内容不正确: %s", string(data))
	}

	// 修改模板文件（push 从 .dec/templates/ 读取）
	templateHelper := filepath.Join(projectTwo, ".dec", "templates", "team-vault", "skills", "my-skill", "helper.py")
	writeVaultTestFile(t, templateHelper, "print('v2')\n")
	if err := runVaultPush(nil, nil); err != nil {
		t.Fatalf("runVaultPush 失败: %v", err)
	}
	refreshInspectRepo(t, inspectDir)
	if data, err := os.ReadFile(filepath.Join(inspectDir, "team-vault", "skills", "my-skill", "helper.py")); err != nil {
		t.Fatalf("推送后远端应存在 helper.py: %v", err)
	} else if !strings.Contains(string(data), "v2") {
		t.Fatalf("推送后内容未更新: %s", string(data))
	}

	oldRemoveRemote := removeRemote
	removeRemote = true
	defer func() { removeRemote = oldRemoveRemote }()
	if err := runVaultRemove(nil, []string{"skill", "my-skill"}); err != nil {
		t.Fatalf("runVaultRemove --remote 失败: %v", err)
	}
	refreshInspectRepo(t, inspectDir)
	if _, err := os.Stat(filepath.Join(inspectDir, "team-vault", "skills", "my-skill")); !os.IsNotExist(err) {
		t.Fatalf("远端 skill 应被删除")
	}
}

func setupVaultRemoveRemoteTestRepo(t *testing.T) (string, string) {
	t.Helper()

	decHome := t.TempDir()
	root := t.TempDir()
	remoteBareDir := filepath.Join(root, "remote.git")
	seedDir := filepath.Join(root, "seed")
	inspectDir := filepath.Join(root, "inspect")
	localBareDir := filepath.Join(decHome, "repo.git")

	runVaultTestGitNoDir(t, "init", "--bare", remoteBareDir)
	runVaultTestGitNoDir(t, "clone", remoteBareDir, seedDir)
	configureVaultTestGitUser(t, seedDir)
	writeVaultTestFile(t, filepath.Join(seedDir, "README.md"), "init\n")
	runVaultTestGit(t, seedDir, "add", ".")
	runVaultTestGit(t, seedDir, "commit", "-m", "initial commit")
	runVaultTestGit(t, seedDir, "branch", "-M", "main")
	runVaultTestGit(t, seedDir, "push", "-u", "origin", "main")
	runVaultTestGitNoDir(t, "--git-dir", remoteBareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	runVaultTestGitNoDir(t, "clone", "--bare", remoteBareDir, localBareDir)
	runVaultTestGitNoDir(t, "--git-dir", localBareDir, "config", "user.name", "Dec Vault Test")
	runVaultTestGitNoDir(t, "--git-dir", localBareDir, "config", "user.email", "dec-vault-test@example.com")

	runVaultTestGitNoDir(t, "clone", remoteBareDir, inspectDir)
	configureVaultTestGitUser(t, inspectDir)
	runVaultTestGit(t, inspectDir, "config", "pull.rebase", "true")

	return decHome, inspectDir
}

func refreshInspectRepo(t *testing.T, dir string) {
	t.Helper()
	runVaultTestGit(t, dir, "pull", "--ff-only")
}

func configureVaultTestGitUser(t *testing.T, dir string) {
	t.Helper()
	runVaultTestGit(t, dir, "config", "user.name", "Dec Vault Test")
	runVaultTestGit(t, dir, "config", "user.email", "dec-vault-test@example.com")
}

func configureVaultTestBareGitUser(t *testing.T, bareDir string) {
	t.Helper()
	runVaultTestGitNoDir(t, "--git-dir", bareDir, "config", "user.name", "Dec Vault Test")
	runVaultTestGitNoDir(t, "--git-dir", bareDir, "config", "user.email", "dec-vault-test@example.com")
}

func writeVaultTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
}

func runVaultTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s 失败: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func runVaultTestGitNoDir(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s 失败: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output))
}

// ========================================
// copyFile / copyDir
// ========================================

func TestCopyFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "test.txt")
	os.WriteFile(srcPath, []byte("hello world"), 0644)

	dstPath := filepath.Join(dstDir, "sub", "test.txt")
	err := copyFile(srcPath, dstPath)
	if err != nil {
		t.Fatalf("复制文件失败: %v", err)
	}

	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("读取目标文件失败: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("内容不匹配: %s", string(data))
	}
}

func TestCopyFile_CreatesParentDirs(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "src.txt")
	os.WriteFile(srcPath, []byte("test"), 0644)

	dstPath := filepath.Join(dstDir, "a", "b", "c", "dst.txt")
	err := copyFile(srcPath, dstPath)
	if err != nil {
		t.Fatalf("应自动创建父目录: %v", err)
	}
	if _, err := os.Stat(dstPath); err != nil {
		t.Fatalf("目标文件不存在: %v", err)
	}
}

func TestCopyDir(t *testing.T) {
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("aaa"), 0644)
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("bbb"), 0644)

	dstDir := filepath.Join(t.TempDir(), "copy")
	err := copyDir(srcDir, dstDir)
	if err != nil {
		t.Fatalf("复制目录失败: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dstDir, "a.txt"))
	if string(data) != "aaa" {
		t.Fatalf("顶层文件内容不匹配")
	}
	data, _ = os.ReadFile(filepath.Join(dstDir, "sub", "b.txt"))
	if string(data) != "bbb" {
		t.Fatalf("子目录文件内容不匹配")
	}
}

func TestCopyDir_OverwritesExisting(t *testing.T) {
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("new"), 0644)

	dstDir := t.TempDir()
	os.WriteFile(filepath.Join(dstDir, "file.txt"), []byte("old"), 0644)

	err := copyDir(srcDir, dstDir)
	if err != nil {
		t.Fatalf("复制目录失败: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dstDir, "file.txt"))
	if string(data) != "new" {
		t.Fatalf("应覆盖已有文件, 得到: %s", string(data))
	}
}

// ========================================
// saveAssetToVault 分发
// ========================================

func TestSaveAssetToVault_Dispatch(t *testing.T) {
	sourceDir := t.TempDir()
	vaultDir := t.TempDir()
	for _, sub := range []string{"skills", "rules", "mcp"} {
		os.MkdirAll(filepath.Join(vaultDir, sub), 0755)
	}

	// skill
	skillDir := filepath.Join(sourceDir, "test-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("test"), 0644)
	name, err := saveAssetToVault("skill", skillDir, vaultDir)
	if err != nil {
		t.Fatalf("保存 skill 失败: %v", err)
	}
	if name != "test-skill" {
		t.Fatalf("skill 名称错误: %s", name)
	}

	// rule
	rulePath := filepath.Join(sourceDir, "test-rule.mdc")
	os.WriteFile(rulePath, []byte("# rule"), 0644)
	name, err = saveAssetToVault("rule", rulePath, vaultDir)
	if err != nil {
		t.Fatalf("保存 rule 失败: %v", err)
	}
	if name != "test-rule" {
		t.Fatalf("rule 名称错误: %s", name)
	}

	// mcp
	mcpPath := filepath.Join(sourceDir, "test-mcp.json")
	os.WriteFile(mcpPath, []byte(`{"command":"node"}`), 0644)
	name, err = saveAssetToVault("mcp", mcpPath, vaultDir)
	if err != nil {
		t.Fatalf("保存 MCP 失败: %v", err)
	}
	if name != "test-mcp" {
		t.Fatalf("MCP 名称错误: %s", name)
	}

	// unknown type
	_, err = saveAssetToVault("unknown", rulePath, vaultDir)
	if err == nil {
		t.Fatalf("未知类型应返回错误")
	}
}

func TestSaveAssetToVault_SourceNotExist(t *testing.T) {
	vaultDir := t.TempDir()
	_, err := saveAssetToVault("rule", "/nonexistent/path.mdc", vaultDir)
	if err == nil {
		t.Fatalf("不存在的源路径应返回错误")
	}
}

// ========================================
// vault list — 详情展示（通过 listVaultAssets 间接测试）
// ========================================

func TestListVaultAssets_DetailOutput(t *testing.T) {
	// 测试 listVaultAssets 返回的资产包含正确的名称和类型
	vaultDir := t.TempDir()

	// 创建多种资产
	os.MkdirAll(filepath.Join(vaultDir, "skills", "api-test"), 0755)
	os.WriteFile(filepath.Join(vaultDir, "skills", "api-test", "SKILL.md"), []byte("test"), 0644)
	os.MkdirAll(filepath.Join(vaultDir, "skills", "code-review"), 0755)
	os.WriteFile(filepath.Join(vaultDir, "skills", "code-review", "SKILL.md"), []byte("test"), 0644)
	os.MkdirAll(filepath.Join(vaultDir, "rules"), 0755)
	os.WriteFile(filepath.Join(vaultDir, "rules", "logging.mdc"), []byte("# logging"), 0644)
	os.WriteFile(filepath.Join(vaultDir, "rules", "security.mdc"), []byte("# security"), 0644)
	os.MkdirAll(filepath.Join(vaultDir, "mcp"), 0755)
	os.WriteFile(filepath.Join(vaultDir, "mcp", "postgres.json"), []byte("{}"), 0644)

	assets := listVaultAssets(vaultDir, "test-vault")

	if len(assets) != 5 {
		t.Fatalf("期望 5 个资产, 得到 %d: %+v", len(assets), assets)
	}

	// 验证每个资产都有名称和类型
	nameTypeMap := map[string]string{}
	for _, a := range assets {
		nameTypeMap[a.Name] = a.Type
		if a.Vault != "test-vault" {
			t.Fatalf("资产 %s 的 Vault 应为 test-vault, 得到 %s", a.Name, a.Vault)
		}
	}

	expectedAssets := map[string]string{
		"api-test":    "skill",
		"code-review": "skill",
		"logging":     "rule",
		"security":    "rule",
		"postgres":    "mcp",
	}
	for name, expectedType := range expectedAssets {
		gotType, ok := nameTypeMap[name]
		if !ok {
			t.Fatalf("缺少资产: %s", name)
		}
		if gotType != expectedType {
			t.Fatalf("资产 %s 类型应为 %s, 得到 %s", name, expectedType, gotType)
		}
	}
}

// ========================================
// vault pull --all（通过 runVaultPullAll 间接测试其核心逻辑）
// ========================================

func TestPullAllCollectsAssetsFromMultipleVaults(t *testing.T) {
	// 测试从多个 Vault 收集所有资产的逻辑
	repoDir := t.TempDir()

	// Vault 1: 2 skills, 1 rule
	os.MkdirAll(filepath.Join(repoDir, "v1", "skills", "skill-a"), 0755)
	os.WriteFile(filepath.Join(repoDir, "v1", "skills", "skill-a", "SKILL.md"), []byte("test"), 0644)
	os.MkdirAll(filepath.Join(repoDir, "v1", "skills", "skill-b"), 0755)
	os.WriteFile(filepath.Join(repoDir, "v1", "skills", "skill-b", "SKILL.md"), []byte("test"), 0644)
	os.MkdirAll(filepath.Join(repoDir, "v1", "rules"), 0755)
	os.WriteFile(filepath.Join(repoDir, "v1", "rules", "rule-a.mdc"), []byte("# rule"), 0644)

	// Vault 2: 1 mcp
	os.MkdirAll(filepath.Join(repoDir, "v2", "mcp"), 0755)
	os.WriteFile(filepath.Join(repoDir, "v2", "mcp", "pg.json"), []byte(`{"command":"npx"}`), 0644)

	// 隐藏目录不应被扫描
	os.MkdirAll(filepath.Join(repoDir, ".git", "skills", "hidden"), 0755)

	// 扫描所有非隐藏目录
	entries, _ := os.ReadDir(repoDir)
	var vaults []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			vaults = append(vaults, entry.Name())
		}
	}

	var allAssets []vaultAssetInfo
	for _, v := range vaults {
		vaultDir := filepath.Join(repoDir, v)
		assets := listVaultAssets(vaultDir, v)
		allAssets = append(allAssets, assets...)
	}

	if len(allAssets) != 4 {
		t.Fatalf("期望 4 个资产（跨 2 个 Vault）, 得到 %d: %+v", len(allAssets), allAssets)
	}

	// 验证资产来自正确的 Vault
	v1Count, v2Count := 0, 0
	for _, a := range allAssets {
		switch a.Vault {
		case "v1":
			v1Count++
		case "v2":
			v2Count++
		}
	}
	if v1Count != 3 {
		t.Fatalf("v1 应有 3 个资产, 得到 %d", v1Count)
	}
	if v2Count != 1 {
		t.Fatalf("v2 应有 1 个资产, 得到 %d", v2Count)
	}
}

func TestPullAllFilterByVault(t *testing.T) {
	// 测试 --vault 过滤：只收集指定 Vault 的资产
	repoDir := t.TempDir()

	// Vault 1
	os.MkdirAll(filepath.Join(repoDir, "v1", "skills", "skill-a"), 0755)
	os.WriteFile(filepath.Join(repoDir, "v1", "skills", "skill-a", "SKILL.md"), []byte("test"), 0644)

	// Vault 2
	os.MkdirAll(filepath.Join(repoDir, "v2", "rules"), 0755)
	os.WriteFile(filepath.Join(repoDir, "v2", "rules", "rule-b.mdc"), []byte("test"), 0644)

	// 只获取 v2 的资产
	targetVaults := []string{"v2"}
	var allAssets []vaultAssetInfo
	for _, v := range targetVaults {
		vaultDir := filepath.Join(repoDir, v)
		assets := listVaultAssets(vaultDir, v)
		allAssets = append(allAssets, assets...)
	}

	if len(allAssets) != 1 {
		t.Fatalf("指定 v2 应只有 1 个资产, 得到 %d", len(allAssets))
	}
	if allAssets[0].Vault != "v2" {
		t.Fatalf("资产应来自 v2, 得到 %s", allAssets[0].Vault)
	}
	if allAssets[0].Name != "rule-b" {
		t.Fatalf("资产名应为 rule-b, 得到 %s", allAssets[0].Name)
	}
}

func TestPullAllInstallsToIDE(t *testing.T) {
	// 测试 pull --all 的完整安装流程
	repoDir := t.TempDir()
	projectRoot := t.TempDir()

	// 创建 Vault 资产
	os.MkdirAll(filepath.Join(repoDir, "v1", "skills", "my-skill"), 0755)
	os.WriteFile(filepath.Join(repoDir, "v1", "skills", "my-skill", "SKILL.md"), []byte("---\nname: my-skill\n---"), 0644)
	os.MkdirAll(filepath.Join(repoDir, "v1", "rules"), 0755)
	os.WriteFile(filepath.Join(repoDir, "v1", "rules", "my-rule.mdc"), []byte("# my rule"), 0644)

	// 收集资产
	assets := listVaultAssets(filepath.Join(repoDir, "v1"), "v1")

	cursorIDE := ide.Get("cursor")

	// 逐个安装
	for _, asset := range assets {
		assetPath := getAssetPath(repoDir, asset.Vault, asset.Type, asset.Name)
		err := installAssetToIDE(asset.Type, asset.Name, assetPath, projectRoot, cursorIDE)
		if err != nil {
			t.Fatalf("安装 %s/%s 失败: %v", asset.Type, asset.Name, err)
		}
	}

	// 验证 skill 已安装
	skillPath := filepath.Join(projectRoot, ".cursor", "skills", "dec-my-skill", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("skill 未安装到 IDE: %v", err)
	}

	// 验证 rule 已安装
	rulePath := filepath.Join(projectRoot, ".cursor", "rules", "dec-my-rule.mdc")
	if _, err := os.Stat(rulePath); err != nil {
		t.Fatalf("rule 未安装到 IDE: %v", err)
	}
}

func TestPullAllEmptyVault(t *testing.T) {
	// 空 Vault 不应产生任何资产
	repoDir := t.TempDir()
	os.MkdirAll(filepath.Join(repoDir, "empty-vault", "skills"), 0755)
	os.MkdirAll(filepath.Join(repoDir, "empty-vault", "rules"), 0755)
	os.MkdirAll(filepath.Join(repoDir, "empty-vault", "mcp"), 0755)

	assets := listVaultAssets(filepath.Join(repoDir, "empty-vault"), "empty-vault")
	if len(assets) != 0 {
		t.Fatalf("空 Vault 应返回 0 个资产, 得到 %d", len(assets))
	}
}
