package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/types"
)

type failingMCPIDE struct {
	name string
}

func (f *failingMCPIDE) Name() string                   { return f.name }
func (f *failingMCPIDE) UserRootDir(home string) string { return filepath.Join(home, "."+f.name) }
func (f *failingMCPIDE) RulesDir(pr string) string      { return filepath.Join(pr, "."+f.name, "rules") }
func (f *failingMCPIDE) SkillsDir(pr string) string     { return filepath.Join(pr, "."+f.name, "skills") }
func (f *failingMCPIDE) MCPConfigPath(pr string) string {
	return filepath.Join(pr, "."+f.name, "mcp.json")
}
func (f *failingMCPIDE) WriteRules(string, []ide.RuleFile) error          { return nil }
func (f *failingMCPIDE) WriteSkill(string, string, []ide.SkillFile) error { return nil }
func (f *failingMCPIDE) WriteMCPConfig(string, *types.MCPConfig) error    { return nil }
func (f *failingMCPIDE) LoadMCPConfig(string) (*types.MCPConfig, error) {
	return nil, fmt.Errorf("mock MCP load failure")
}

// ========================================
// isValidAssetType
// ========================================

func TestIsValidAssetType(t *testing.T) {
	for _, v := range []string{"skill", "rule", "mcp", "bundle"} {
		if !isValidAssetType(v) {
			t.Fatalf("期望 %q 合法", v)
		}
	}
	for _, v := range []string{"", "skills", "bundles", "unknown"} {
		if isValidAssetType(v) {
			t.Fatalf("期望 %q 不合法", v)
		}
	}
}

// ========================================
// managedName
// ========================================

func TestManagedName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"my-skill", "dec-my-skill"},
		{"dec-my-skill", "dec-my-skill"},
	}
	for _, tt := range tests {
		if got := managedName(tt.in); got != tt.want {
			t.Fatalf("managedName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ========================================
// resolveAssetFile
// ========================================

func TestResolveAssetFile(t *testing.T) {
	repoDir := "/fake/repo"
	tests := []struct {
		itemType, assetName, want string
	}{
		{"skill", "my-skill", filepath.Join(repoDir, "cli/skills", "my-skill")},
		{"rule", "my-rule", filepath.Join(repoDir, "cli/rules", "my-rule.mdc")},
		{"mcp", "my-mcp", filepath.Join(repoDir, "cli/mcp", "my-mcp.json")},
		{"bundle", "my-bundle", filepath.Join(repoDir, "cli/bundles", "my-bundle.yaml")},
		{"unknown", "x", ""},
	}
	for _, tt := range tests {
		got := resolveAssetFile(repoDir, "cli", tt.itemType, tt.assetName)
		if got != tt.want {
			t.Fatalf("resolveAssetFile(%q, %q) = %q, want %q", tt.itemType, tt.assetName, got, tt.want)
		}
	}
}

// ========================================
// listFolderAssets
// ========================================

func TestListFolderAssets(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "skills", "api-test"), 0755)
	os.WriteFile(filepath.Join(dir, "skills", "api-test", "SKILL.md"), []byte("test"), 0644)
	os.MkdirAll(filepath.Join(dir, "rules"), 0755)
	os.WriteFile(filepath.Join(dir, "rules", "logging.mdc"), []byte("# logging"), 0644)
	os.WriteFile(filepath.Join(dir, "rules", ".gitkeep"), []byte(""), 0644)
	os.MkdirAll(filepath.Join(dir, "mcp"), 0755)
	os.WriteFile(filepath.Join(dir, "mcp", "postgres.json"), []byte("{}"), 0644)

	assets := listFolderAssets(dir, "test")
	if len(assets) != 3 {
		t.Fatalf("期望 3 个, 得到 %d", len(assets))
	}
	found := map[string]string{}
	for _, a := range assets {
		found[a.Name] = a.Type
		if a.Name == ".gitkeep" {
			t.Fatal(".gitkeep 不应出现")
		}
	}
	if found["api-test"] != "skill" {
		t.Fatalf("api-test 应为 skill")
	}
	if found["logging"] != "rule" {
		t.Fatalf("logging 应为 rule")
	}
	if found["postgres"] != "mcp" {
		t.Fatalf("postgres 应为 mcp")
	}
}

func TestListFolderAssets_Empty(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"skills", "rules", "mcp"} {
		os.MkdirAll(filepath.Join(dir, sub), 0755)
		os.WriteFile(filepath.Join(dir, sub, ".gitkeep"), []byte(""), 0644)
	}
	if len(listFolderAssets(dir, "empty")) != 0 {
		t.Fatal("空目录应返回 0")
	}
}

func TestListFolderAssets_Path(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "rules"), 0755)
	os.WriteFile(filepath.Join(dir, "rules", "r1.mdc"), []byte("test"), 0644)

	assets := listFolderAssets(dir, "cli")
	if len(assets) != 1 {
		t.Fatalf("期望 1 个, 得到 %d", len(assets))
	}
	if assets[0].Vault != "cli" {
		t.Fatalf("vault 应为 cli, 得到 %s", assets[0].Vault)
	}
}

// ========================================
// readFolderEntries
// ========================================

func TestReadFolderEntries(t *testing.T) {
	repoDir := t.TempDir()
	os.MkdirAll(filepath.Join(repoDir, "v1"), 0755)
	os.MkdirAll(filepath.Join(repoDir, "v2"), 0755)
	os.MkdirAll(filepath.Join(repoDir, ".git"), 0755)

	entries, err := readFolderEntries(repoDir)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("期望 2, 得到 %d", len(entries))
	}
}

// ========================================
// getCachePath
// ========================================

func TestGetCachePath(t *testing.T) {
	root := "/project"
	tests := []struct {
		itemType, assetName, want string
	}{
		{"skill", "my-skill", filepath.Join(root, ".dec", "cache", "cli/skills", "my-skill")},
		{"rule", "my-rule", filepath.Join(root, ".dec", "cache", "cli/rules", "my-rule.mdc")},
		{"mcp", "my-mcp", filepath.Join(root, ".dec", "cache", "cli/mcp", "my-mcp.json")},
		{"bundle", "my-bundle", filepath.Join(root, ".dec", "cache", "cli/bundles", "my-bundle.yaml")},
		{"unknown", "x", ""},
	}
	for _, tt := range tests {
		got := getCachePath(root, "cli", tt.itemType, tt.assetName)
		if got != tt.want {
			t.Fatalf("getCachePath(%q, %q) = %q, want %q", tt.itemType, tt.assetName, got, tt.want)
		}
	}
}

// ========================================
// findAssetInRepo
// ========================================

func TestFindAssetInRepo(t *testing.T) {
	repoDir := t.TempDir()
	os.MkdirAll(filepath.Join(repoDir, "cli", "rules"), 0755)
	os.WriteFile(filepath.Join(repoDir, "cli", "rules", "my-rule.mdc"), []byte("test"), 0644)

	vault, fullPath, err := findAssetInRepo(repoDir, "rule", "my-rule")
	if err != nil {
		t.Fatalf("查找失败: %v", err)
	}
	if vault != "cli" {
		t.Fatalf("vault 应为 cli, 得到 %s", vault)
	}
	if !strings.HasSuffix(fullPath, "cli/rules/my-rule.mdc") {
		t.Fatalf("fullPath 不对: %s", fullPath)
	}
}

func TestFindAssetInRepo_NotFound(t *testing.T) {
	repoDir := t.TempDir()
	os.MkdirAll(filepath.Join(repoDir, "v1", "skills"), 0755)

	_, _, err := findAssetInRepo(repoDir, "skill", "nonexistent")
	if err == nil {
		t.Fatal("不存在应返回错误")
	}
}

// ========================================
// installAssetToIDE / removeAssetFromIDE
// ========================================

func TestInstallSkillToIDE(t *testing.T) {
	projectRoot := t.TempDir()
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("test"), 0644)

	cursorIDE := ide.Get("cursor")
	if err := installAssetToIDE("skill", "my-skill", srcDir, projectRoot, cursorIDE); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-my-skill", "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md 未安装: %v", err)
	}
}

func TestInstallRuleToIDE(t *testing.T) {
	projectRoot := t.TempDir()
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "logging.mdc")
	os.WriteFile(srcPath, []byte("# rule"), 0644)

	cursorIDE := ide.Get("cursor")
	if err := installAssetToIDE("rule", "logging", srcPath, projectRoot, cursorIDE); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(projectRoot, ".cursor", "rules", "dec-logging.mdc"))
	if string(data) != "# rule" {
		t.Fatalf("内容不匹配")
	}
}

func TestInstallMCPToIDE(t *testing.T) {
	projectRoot := t.TempDir()
	mcpDir := filepath.Join(projectRoot, ".cursor")
	os.MkdirAll(mcpDir, 0755)
	data, _ := json.Marshal(types.MCPConfig{MCPServers: map[string]types.MCPServer{"user": {Command: "npx"}}})
	os.WriteFile(filepath.Join(mcpDir, "mcp.json"), data, 0644)

	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "pg.json"), []byte(`{"command":"npx","args":["-y","pg"]}`), 0644)

	cursorIDE := ide.Get("cursor")
	if err := installAssetToIDE("mcp", "pg", filepath.Join(srcDir, "pg.json"), projectRoot, cursorIDE); err != nil {
		t.Fatalf("安装失败: %v", err)
	}

	resultData, _ := os.ReadFile(filepath.Join(mcpDir, "mcp.json"))
	var result types.MCPConfig
	json.Unmarshal(resultData, &result)
	if _, ok := result.MCPServers["user"]; !ok {
		t.Fatal("用户条目被覆盖")
	}
	if _, ok := result.MCPServers["dec-pg"]; !ok {
		t.Fatal("托管条目未写入")
	}
}

func TestInstallMCPToCodexInternalIDEUsesProjectCodexConfig(t *testing.T) {
	projectRoot := t.TempDir()
	configPath := filepath.Join(projectRoot, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("创建 .codex 目录失败: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`model = "gpt-5.4"

[mcp_servers.user]
command = "npx"
`), 0644); err != nil {
		t.Fatalf("写入现有 Codex config.toml 失败: %v", err)
	}

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "pg.json")
	if err := os.WriteFile(srcPath, []byte(`{"command":"npx","args":["-y","pg"]}`), 0644); err != nil {
		t.Fatalf("写入 MCP 片段失败: %v", err)
	}

	impl := ide.Get("codex-internal")
	if err := installAssetToIDE("mcp", "pg", srcPath, projectRoot, impl); err != nil {
		t.Fatalf("安装失败: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取 Codex config.toml 失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `[mcp_servers.user]`) {
		t.Fatalf("用户自定义 Codex MCP 配置不应丢失:\n%s", content)
	}
	if !strings.Contains(content, `[mcp_servers.dec-pg]`) {
		t.Fatalf("托管的 Codex MCP 配置未写入 .codex/config.toml:\n%s", content)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".codex-internal", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("项目级 codex-internal 不应写入 .codex-internal/config.toml: %v", err)
	}
}

func TestInstallMCPToClaudeInternalIDEUsesProjectClaudeConfig(t *testing.T) {
	projectRoot := t.TempDir()
	configPath := filepath.Join(projectRoot, ".claude", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("创建 .claude 目录失败: %v", err)
	}
	data, err := json.Marshal(types.MCPConfig{MCPServers: map[string]types.MCPServer{"user": {Command: "npx"}}})
	if err != nil {
		t.Fatalf("序列化现有 Claude MCP 配置失败: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("写入现有 .claude/mcp.json 失败: %v", err)
	}

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "pg.json")
	if err := os.WriteFile(srcPath, []byte(`{"command":"npx","args":["-y","pg"]}`), 0644); err != nil {
		t.Fatalf("写入 MCP 片段失败: %v", err)
	}

	impl := ide.Get("claude-internal")
	if err := installAssetToIDE("mcp", "pg", srcPath, projectRoot, impl); err != nil {
		t.Fatalf("安装失败: %v", err)
	}

	resultData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取 .claude/mcp.json 失败: %v", err)
	}
	var result types.MCPConfig
	if err := json.Unmarshal(resultData, &result); err != nil {
		t.Fatalf("解析 .claude/mcp.json 失败: %v", err)
	}
	if _, ok := result.MCPServers["user"]; !ok {
		t.Fatal("用户条目被覆盖")
	}
	if _, ok := result.MCPServers["dec-pg"]; !ok {
		t.Fatal("托管条目未写入 .claude/mcp.json")
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".claude-internal", "mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("项目级 claude-internal 不应写入 .claude-internal/mcp.json: %v", err)
	}
}

func TestRemoveMCPFromCodexIDEPreservesUserConfig(t *testing.T) {
	projectRoot := t.TempDir()
	configPath := filepath.Join(projectRoot, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("创建 .codex 目录失败: %v", err)
	}
	content := `[mcp_servers.user]
command = "user"

[mcp_servers.dec-pg]
command = "npx"
args = ["-y","pg"]
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("写入 Codex config.toml 失败: %v", err)
	}

	impl := ide.Get("codex")
	removed, err := removeAssetFromIDE("mcp", "pg", projectRoot, impl)
	if err != nil {
		t.Fatalf("移除失败: %v", err)
	}
	if !removed {
		t.Fatal("应返回 true")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取 Codex config.toml 失败: %v", err)
	}
	result := string(data)
	if !strings.Contains(result, `[mcp_servers.user]`) {
		t.Fatalf("用户自定义 Codex MCP 配置不应被删除:\n%s", result)
	}
	if strings.Contains(result, `[mcp_servers.dec-pg]`) {
		t.Fatalf("托管的 Codex MCP 配置应被删除:\n%s", result)
	}
}

func TestRemoveSkillFromIDE(t *testing.T) {
	projectRoot := t.TempDir()
	skillDir := filepath.Join(projectRoot, ".cursor", "skills", "dec-my-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("test"), 0644)

	cursorIDE := ide.Get("cursor")
	removed, err := removeAssetFromIDE("skill", "my-skill", projectRoot, cursorIDE)
	if err != nil {
		t.Fatalf("移除失败: %v", err)
	}
	if !removed {
		t.Fatal("应返回 true")
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatal("应被删除")
	}
}

func TestRemoveAssetFromIDE_NotExists(t *testing.T) {
	projectRoot := t.TempDir()
	cursorIDE := ide.Get("cursor")
	if _, err := removeAssetFromIDE("skill", "nonexistent", projectRoot, cursorIDE); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
}

// ========================================
// copyFile / copyDir
// ========================================

func TestCopyFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("hello"), 0644)

	if err := copyFile(filepath.Join(srcDir, "test.txt"), filepath.Join(dstDir, "sub", "test.txt")); err != nil {
		t.Fatalf("复制失败: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dstDir, "sub", "test.txt"))
	if string(data) != "hello" {
		t.Fatalf("内容不匹配")
	}
}

func TestCopyDir(t *testing.T) {
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("aaa"), 0644)
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("bbb"), 0644)

	dstDir := filepath.Join(t.TempDir(), "copy")
	if err := copyDir(srcDir, dstDir); err != nil {
		t.Fatalf("复制失败: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dstDir, "sub", "b.txt"))
	if string(data) != "bbb" {
		t.Fatal("子目录内容不匹配")
	}
}

// ========================================
// AssetList methods
// ========================================

func TestAssetListDedup(t *testing.T) {
	list := &types.AssetList{
		Rules: []types.AssetRef{
			{Name: "r1", Vault: "v1"},
			{Name: "r1", Vault: "v2"},
			{Name: "r1", Vault: "v2"},
			{Name: "r2", Vault: "v1"},
		},
	}
	list.Dedup()
	if len(list.Rules) != 3 {
		t.Fatalf("不同 vault 的同名资产应保留，重复项才去重，得到 %d", len(list.Rules))
	}
	if list.Rules[0].Vault != "v1" {
		t.Fatalf("第一个 r1 应保留 v1, 得到 %s", list.Rules[0].Vault)
	}
	if list.Rules[1].Vault != "v2" {
		t.Fatalf("第二个 r1 应保留 v2, 得到 %s", list.Rules[1].Vault)
	}
	if list.Rules[2].Name != "r2" {
		t.Fatalf("最后一个应为 r2, 得到 %s", list.Rules[2].Name)
	}
}

func TestAssetListIsEmpty(t *testing.T) {
	var nilList *types.AssetList
	if !nilList.IsEmpty() {
		t.Fatal("nil 应为空")
	}
	if !(&types.AssetList{}).IsEmpty() {
		t.Fatal("空列表应为空")
	}
	list := &types.AssetList{Rules: []types.AssetRef{{Name: "r"}}}
	if list.IsEmpty() {
		t.Fatal("有内容不应为空")
	}
}

func TestAssetListAll(t *testing.T) {
	list := &types.AssetList{
		Skills: []types.AssetRef{{Name: "s1"}},
		Rules:  []types.AssetRef{{Name: "r1"}, {Name: "r2"}},
		MCPs:   []types.AssetRef{{Name: "m1"}},
	}
	all := list.All()
	if len(all) != 4 {
		t.Fatalf("期望 4, 得到 %d", len(all))
	}
}

func TestAssetListFindAndRemove(t *testing.T) {
	list := &types.AssetList{
		Rules: []types.AssetRef{{Name: "r1", Vault: "v1"}, {Name: "r1", Vault: "v2"}, {Name: "r2", Vault: "v1"}},
	}
	if ref := list.FindAsset("rule", "r1", "v2"); ref == nil || ref.Vault != "v2" {
		t.Fatal("应找到 v2 中的 r1")
	}
	if ref := list.FindAsset("rule", "r1"); ref == nil {
		t.Fatal("应找到 r1")
	}
	if list.RemoveAsset("rule", "r1", "v2") != true {
		t.Fatal("应成功移除")
	}
	if len(list.Rules) != 2 {
		t.Fatalf("移除后应剩 2 个, 得到 %d", len(list.Rules))
	}
	if list.FindAsset("rule", "r1", "v2") != nil {
		t.Fatal("v2 中的 r1 应已被移除")
	}
	if list.FindAsset("rule", "r1", "v1") == nil {
		t.Fatal("v1 中的 r1 不应被误删")
	}
}

func TestPrintMissingVars_IncludesConcreteFileTargets(t *testing.T) {
	projectRoot := t.TempDir()
	projectVarsPath := filepath.Join(projectRoot, ".dec", "vars.yaml")
	globalVarsPath := filepath.Join(projectRoot, ".dec-home", "local", "vars.yaml")

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建 stdout pipe 失败: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	printMissingVars(
		"skill",
		"vikunja-workflow",
		[]string{"TASK_DOCS_DIR", "TASK_DOCS_DIR"},
		map[string][]string{
			"TASK_DOCS_DIR": {
				filepath.Join(projectRoot, ".cursor", "skills", "dec-vikunja-workflow", "SKILL.md"),
				filepath.Join(projectRoot, ".cursor", "skills", "dec-vikunja-workflow", "templates", "task.md"),
			},
		},
		projectVarsPath,
		globalVarsPath,
	)

	if err := w.Close(); err != nil {
		t.Fatalf("关闭写端失败: %v", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("读取输出失败: %v", err)
	}

	out := buf.String()
	if strings.Count(out, "变量 {{TASK_DOCS_DIR}} 未定义") != 1 {
		t.Fatalf("缺失变量提示应去重, 实际输出:\n%s", out)
	}
	for _, want := range []string{
		"资产: [skill] vikunja-workflow",
		filepath.Clean(filepath.Join(projectRoot, ".cursor", "skills", "dec-vikunja-workflow", "SKILL.md")),
		filepath.Clean(filepath.Join(projectRoot, ".cursor", "skills", "dec-vikunja-workflow", "templates", "task.md")),
		projectVarsPath + " -> vars.TASK_DOCS_DIR 或 assets.skill.vikunja-workflow.vars.TASK_DOCS_DIR",
		globalVarsPath + " -> vars.TASK_DOCS_DIR 或 assets.skill.vikunja-workflow.vars.TASK_DOCS_DIR",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("输出缺少 %q, 实际输出:\n%s", want, out)
		}
	}
	if strings.Index(out, "SKILL.md") > strings.Index(out, "templates/task.md") {
		t.Fatalf("来源路径应按字典序稳定输出, 实际输出:\n%s", out)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("关闭读端失败: %v", err)
	}
}

// suppress unused
var _ = strings.Contains

// chdirForTest 切换工作目录，测试结束后恢复
func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取当前目录失败: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("切换目录失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
}
