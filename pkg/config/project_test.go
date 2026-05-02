package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

func TestSaveAndLoadProjectConfig(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	cfg := &types.ProjectConfig{
		IDEs:   []string{"cursor"},
		Editor: "vim",
		Available: &types.AssetList{
			Rules: []types.AssetRef{
				{Name: "rule-a", Vault: "v1"},
				{Name: "rule-b", Vault: "v2"},
			},
		},
		Enabled: &types.AssetList{
			Rules: []types.AssetRef{
				{Name: "rule-a", Vault: "v1"},
			},
		},
	}

	if err := mgr.SaveProjectConfig(cfg); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(projectRoot, ".dec", "config.yaml"))
	if err != nil {
		t.Fatalf("读取保存后的配置失败: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "#   ides:") || !strings.Contains(content, "#   editor: code --wait") {
		t.Fatalf("项目配置头注释应包含 ides/editor 示例, 实际内容:\n%s", content)
	}
	if !strings.Contains(content, "version: v2") {
		t.Fatalf("保存后的配置应写入 version: v2, 实际内容:\n%s", content)
	}
	if !strings.Contains(content, "v1:") || !strings.Contains(content, "rule-a:") || !strings.Contains(content, "rules: true") {
		t.Fatalf("保存后的配置应使用 v2 的 vault/item/type 结构, 实际内容:\n%s", content)
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	if loaded.Available.Count() != 2 {
		t.Fatalf("available 应有 2 个, 得到 %d", loaded.Available.Count())
	}
	if loaded.Enabled.Count() != 1 {
		t.Fatalf("enabled 应有 1 个, 得到 %d", loaded.Enabled.Count())
	}
	if loaded.Editor != "vim" {
		t.Fatalf("editor = %q, 期望 %q", loaded.Editor, "vim")
	}
	if loaded.IDEs[0] != "cursor" {
		t.Fatalf("ides[0] = %q, 期望 %q", loaded.IDEs[0], "cursor")
	}
	if loaded.Version != types.ProjectConfigVersionV2 {
		t.Fatalf("version = %q, 期望 %q", loaded.Version, types.ProjectConfigVersionV2)
	}
}

func TestLoadProjectConfig_Dedup(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	// 手写一个有重复的配置
	decDir := filepath.Join(projectRoot, ".dec")
	os.MkdirAll(decDir, 0755)
	content := `
version: v2
enabled:
  v1:
    rule-a:
      rules: true
      rules: true
`
	os.WriteFile(filepath.Join(decDir, "config.yaml"), []byte(content), 0644)

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	// v2 同一 vault/item/type 重复声明后应只保留 1 个
	if loaded.Enabled.Count() != 1 {
		t.Fatalf("去重后应有 1 个, 得到 %d", loaded.Enabled.Count())
	}
	if loaded.Enabled.Rules[0].Vault != "v1" {
		t.Fatalf("vault = %s, 期望 v1", loaded.Enabled.Rules[0].Vault)
	}
}

func TestLoadProjectConfig_MigratesV1ToV2(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	decDir := filepath.Join(projectRoot, ".dec")
	if err := os.MkdirAll(decDir, 0755); err != nil {
		t.Fatalf("创建 .dec 目录失败: %v", err)
	}

	legacy := `
ides:
  - cursor
editor: vim
available:
  rules:
    - name: shared-rule
      vault: team
  mcps:
    - name: postgres
      vault: infra
enabled:
  rules:
    - name: shared-rule
      vault: team
`
	configPath := filepath.Join(decDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(legacy), 0644); err != nil {
		t.Fatalf("写入 v1 配置失败: %v", err)
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("加载并迁移 v1 配置失败: %v", err)
	}

	if loaded.Version != types.ProjectConfigVersionV2 {
		t.Fatalf("version = %q, 期望 %q", loaded.Version, types.ProjectConfigVersionV2)
	}
	if loaded.Editor != "vim" {
		t.Fatalf("editor = %q, 期望 vim", loaded.Editor)
	}
	if len(loaded.IDEs) != 1 || loaded.IDEs[0] != "cursor" {
		t.Fatalf("ides = %#v, 期望 [cursor]", loaded.IDEs)
	}
	if loaded.Available.FindAsset("rule", "shared-rule", "team") == nil {
		t.Fatal("迁移后应保留 available 中的 rule")
	}
	if loaded.Available.FindAsset("mcp", "postgres", "infra") == nil {
		t.Fatal("迁移后应保留 available 中的 mcp")
	}
	if loaded.Enabled.FindAsset("rule", "shared-rule", "team") == nil {
		t.Fatal("迁移后应保留 enabled 中的 rule")
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取迁移后的配置失败: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "version: v2") {
		t.Fatalf("迁移后配置应写入 version: v2, 实际内容:\n%s", content)
	}
	if !strings.Contains(content, "team:") || !strings.Contains(content, "shared-rule:") || !strings.Contains(content, "rules: true") {
		t.Fatalf("迁移后配置应使用 v2 结构, 实际内容:\n%s", content)
	}
	if strings.Contains(content, "- name:") {
		t.Fatalf("迁移后不应保留 v1 列表结构, 实际内容:\n%s", content)
	}
}

func TestSaveProjectConfig_DoesNotModifyGitignore(t *testing.T) {
	projectRoot := t.TempDir()
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	original := "node_modules/\n.cursor/\n"
	os.WriteFile(gitignorePath, []byte(original), 0644)

	mgr := NewProjectConfigManager(projectRoot)
	cfg := &types.ProjectConfig{}
	if err := mgr.SaveProjectConfig(cfg); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	data, _ := os.ReadFile(gitignorePath)
	if string(data) != original {
		t.Fatalf("不应修改 .gitignore")
	}
}

func TestEnsureVarsConfigTemplate_CreatesDefaultFile(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	created, err := mgr.EnsureVarsConfigTemplate()
	if err != nil {
		t.Fatalf("EnsureVarsConfigTemplate() 失败: %v", err)
	}
	if !created {
		t.Fatal("首次调用应创建 vars.yaml")
	}

	data, err := os.ReadFile(mgr.GetVarsPath())
	if err != nil {
		t.Fatalf("读取 vars.yaml 失败: %v", err)
	}
	content := string(data)
	if content == "" {
		t.Fatal("vars.yaml 不应为空")
	}
	if !containsAll(content, []string{"vars:", "assets:", "{{VAR_NAME}}", "skill:", "rule:", "mcp:"}) {
		t.Fatalf("vars.yaml 模板内容不完整: %q", content)
	}

	created, err = mgr.EnsureVarsConfigTemplate()
	if err != nil {
		t.Fatalf("EnsureVarsConfigTemplate() 二次调用失败: %v", err)
	}
	if created {
		t.Fatal("已有 vars.yaml 时不应重复创建")
	}
}

func containsAll(content string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(content, part) {
			return false
		}
	}
	return true
}

// ========================================
// LoadVarsConfig + vars.d/ 合并语义
// ========================================

// writeFile 写入文件，父目录必须已存在；测试辅助函数。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写入 %s 失败: %v", path, err)
	}
}

// mkdirVarsD 创建 .dec/vars.d/ 目录并返回路径。
func mkdirVarsD(t *testing.T, projectRoot string) string {
	t.Helper()
	dir := filepath.Join(projectRoot, ".dec", "vars.d")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建 vars.d 目录失败: %v", err)
	}
	return dir
}

// mkdirDec 创建 .dec/ 目录并返回路径。
func mkdirDec(t *testing.T, projectRoot string) string {
	t.Helper()
	dir := filepath.Join(projectRoot, ".dec")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建 .dec 目录失败: %v", err)
	}
	return dir
}

// 用例 1：仅 vars.yaml，没有 vars.d/（回归）
func TestLoadVarsConfig_OnlyMainFile(t *testing.T) {
	projectRoot := t.TempDir()
	decDir := mkdirDec(t, projectRoot)
	writeFile(t, filepath.Join(decDir, "vars.yaml"), "vars:\n  FOO: bar\n")

	mgr := NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadVarsConfig()
	if err != nil {
		t.Fatalf("LoadVarsConfig() 失败: %v", err)
	}
	if cfg.Vars["FOO"] != "bar" {
		t.Fatalf("FOO = %q, 期望 bar", cfg.Vars["FOO"])
	}
	if len(cfg.Vars) != 1 {
		t.Fatalf("vars 长度 = %d, 期望 1", len(cfg.Vars))
	}
}

// 用例 2：仅 vars.d/a.yaml，无主文件
func TestLoadVarsConfig_OnlyFragment(t *testing.T) {
	projectRoot := t.TempDir()
	vd := mkdirVarsD(t, projectRoot)
	writeFile(t, filepath.Join(vd, "a.yaml"), "vars:\n  FOO: from-fragment\n")

	mgr := NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadVarsConfig()
	if err != nil {
		t.Fatalf("LoadVarsConfig() 失败: %v", err)
	}
	if cfg.Vars["FOO"] != "from-fragment" {
		t.Fatalf("FOO = %q, 期望 from-fragment", cfg.Vars["FOO"])
	}
}

// 用例 3：vars.d/a.yaml + vars.d/b.yaml 同键，b 后加载胜出
func TestLoadVarsConfig_FragmentOrderLaterWins(t *testing.T) {
	projectRoot := t.TempDir()
	vd := mkdirVarsD(t, projectRoot)
	writeFile(t, filepath.Join(vd, "a.yaml"), "vars:\n  FOO: from-a\n")
	writeFile(t, filepath.Join(vd, "b.yaml"), "vars:\n  FOO: from-b\n")

	mgr := NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadVarsConfig()
	if err != nil {
		t.Fatalf("LoadVarsConfig() 失败: %v", err)
	}
	if cfg.Vars["FOO"] != "from-b" {
		t.Fatalf("FOO = %q, 期望 from-b (字典序后胜出)", cfg.Vars["FOO"])
	}
}

// 用例 4：fragment 中的 X 与 vars.yaml 中的 X 冲突，主文件胜出
func TestLoadVarsConfig_MainOverridesFragment(t *testing.T) {
	projectRoot := t.TempDir()
	decDir := mkdirDec(t, projectRoot)
	vd := mkdirVarsD(t, projectRoot)
	writeFile(t, filepath.Join(vd, "a.yaml"), "vars:\n  FOO: from-fragment\n  BAR: only-in-fragment\n")
	writeFile(t, filepath.Join(decDir, "vars.yaml"), "vars:\n  FOO: from-main\n")

	mgr := NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadVarsConfig()
	if err != nil {
		t.Fatalf("LoadVarsConfig() 失败: %v", err)
	}
	if cfg.Vars["FOO"] != "from-main" {
		t.Fatalf("FOO = %q, 期望 from-main (主文件覆盖)", cfg.Vars["FOO"])
	}
	if cfg.Vars["BAR"] != "only-in-fragment" {
		t.Fatalf("BAR = %q, 期望 only-in-fragment (fragment 保留)", cfg.Vars["BAR"])
	}
}

// 用例 5：01-foo、10-bar、02-baz 按字典序加载 01 → 02 → 10
func TestLoadVarsConfig_FragmentDictOrder(t *testing.T) {
	projectRoot := t.TempDir()
	vd := mkdirVarsD(t, projectRoot)
	writeFile(t, filepath.Join(vd, "01-foo.yaml"), "vars:\n  KEY: one\n")
	writeFile(t, filepath.Join(vd, "10-bar.yaml"), "vars:\n  KEY: ten\n")
	writeFile(t, filepath.Join(vd, "02-baz.yaml"), "vars:\n  KEY: two\n")

	mgr := NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadVarsConfig()
	if err != nil {
		t.Fatalf("LoadVarsConfig() 失败: %v", err)
	}
	// 字典序：01 → 02 → 10，10 最后胜出
	if cfg.Vars["KEY"] != "ten" {
		t.Fatalf("KEY = %q, 期望 ten (01→02→10 字典序，10 最后胜出)", cfg.Vars["KEY"])
	}
}

// 用例 6：fragment YAML 语法错误，整体返回 error 且 error 含文件名
func TestLoadVarsConfig_FragmentInvalidYAML(t *testing.T) {
	projectRoot := t.TempDir()
	vd := mkdirVarsD(t, projectRoot)
	writeFile(t, filepath.Join(vd, "broken.yaml"), "vars:\n  FOO: [unclosed\n")

	mgr := NewProjectConfigManager(projectRoot)
	_, err := mgr.LoadVarsConfig()
	if err == nil {
		t.Fatal("期望解析失败返回 error, 但得到 nil")
	}
	if !strings.Contains(err.Error(), "broken.yaml") {
		t.Fatalf("error 应包含 fragment 文件名, 得到: %v", err)
	}
}

// 用例 7：隐藏文件、非 yaml 扩展、子目录都应被跳过
func TestLoadVarsConfig_FragmentSkipInvalidEntries(t *testing.T) {
	projectRoot := t.TempDir()
	vd := mkdirVarsD(t, projectRoot)
	// 合法片段
	writeFile(t, filepath.Join(vd, "keep.yaml"), "vars:\n  KEEP: yes\n")
	// 隐藏文件 - 内容故意写坏，若被加载会触发解析错误
	writeFile(t, filepath.Join(vd, ".hidden.yaml"), "vars:\n  X: [broken\n")
	// 非 yaml 扩展名 - 同样故意写坏
	writeFile(t, filepath.Join(vd, "a.txt"), "not yaml at all {{{")
	// 子目录 - ReadDir 会返回但应被 IsDir() 过滤
	subDir := filepath.Join(vd, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("创建子目录失败: %v", err)
	}
	writeFile(t, filepath.Join(subDir, "nested.yaml"), "vars:\n  NESTED: should-not-load\n")

	mgr := NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadVarsConfig()
	if err != nil {
		t.Fatalf("LoadVarsConfig() 失败: %v (隐藏/txt/子目录应被跳过)", err)
	}
	if cfg.Vars["KEEP"] != "yes" {
		t.Fatalf("KEEP = %q, 期望 yes", cfg.Vars["KEEP"])
	}
	if _, ok := cfg.Vars["NESTED"]; ok {
		t.Fatal("子目录内的 fragment 不应被加载")
	}
	if _, ok := cfg.Vars["X"]; ok {
		t.Fatal("隐藏文件不应被加载")
	}
}

// 用例 8：.yml 扩展名也被加载
func TestLoadVarsConfig_FragmentYmlExtension(t *testing.T) {
	projectRoot := t.TempDir()
	vd := mkdirVarsD(t, projectRoot)
	writeFile(t, filepath.Join(vd, "short.yml"), "vars:\n  SHORT: ok\n")

	mgr := NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadVarsConfig()
	if err != nil {
		t.Fatalf("LoadVarsConfig() 失败: %v", err)
	}
	if cfg.Vars["SHORT"] != "ok" {
		t.Fatalf("SHORT = %q, 期望 ok (.yml 应被加载)", cfg.Vars["SHORT"])
	}
}

// 用例 9：fragment 带 assets: 字段，不应污染最终 Assets
func TestLoadVarsConfig_FragmentAssetsIgnored(t *testing.T) {
	projectRoot := t.TempDir()
	decDir := mkdirDec(t, projectRoot)
	vd := mkdirVarsD(t, projectRoot)
	// fragment 带 assets，应被忽略
	writeFile(t, filepath.Join(vd, "a.yaml"), `vars:
  FOO: bar
assets:
  skill:
    my-skill:
      vars:
        SHOULD: be-ignored
`)
	// 主文件有自己的 assets
	writeFile(t, filepath.Join(decDir, "vars.yaml"), `vars:
  MAIN: ok
assets:
  rule:
    real-rule:
      vars:
        FROM: main
`)

	mgr := NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadVarsConfig()
	if err != nil {
		t.Fatalf("LoadVarsConfig() 失败: %v", err)
	}
	if cfg.Vars["FOO"] != "bar" {
		t.Fatalf("FOO = %q, 期望 bar (fragment vars 仍然合并)", cfg.Vars["FOO"])
	}
	if cfg.Vars["MAIN"] != "ok" {
		t.Fatalf("MAIN = %q, 期望 ok", cfg.Vars["MAIN"])
	}
	if cfg.Assets == nil {
		t.Fatal("Assets 应来自主文件, 不应为 nil")
	}
	// fragment 里的 skill 不应出现
	if cfg.Assets.Skills != nil {
		if _, exists := cfg.Assets.Skills["my-skill"]; exists {
			t.Fatal("fragment 中的 assets.skill 不应污染最终 Assets")
		}
	}
	// 主文件里的 rule 应存在
	if cfg.Assets.Rules == nil {
		t.Fatal("主文件中的 assets.rule 应被保留")
	}
	entry, ok := cfg.Assets.Rules["real-rule"]
	if !ok {
		t.Fatal("主文件中的 assets.rule.real-rule 应被保留")
	}
	if entry.Vars["FROM"] != "main" {
		t.Fatalf("real-rule.vars.FROM = %q, 期望 main", entry.Vars["FROM"])
	}
}

// 用例 10：fragment 是空文件，不报错且无键被加入
func TestLoadVarsConfig_FragmentEmptyFile(t *testing.T) {
	projectRoot := t.TempDir()
	vd := mkdirVarsD(t, projectRoot)
	writeFile(t, filepath.Join(vd, "empty.yaml"), "")
	writeFile(t, filepath.Join(vd, "real.yaml"), "vars:\n  REAL: ok\n")

	mgr := NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadVarsConfig()
	if err != nil {
		t.Fatalf("LoadVarsConfig() 失败: %v (空文件应被容忍)", err)
	}
	if cfg.Vars["REAL"] != "ok" {
		t.Fatalf("REAL = %q, 期望 ok", cfg.Vars["REAL"])
	}
}

// 用例 11：vars.d/ 目录不存在，退化为仅读 vars.yaml 的旧行为
func TestLoadVarsConfig_NoVarsDDir(t *testing.T) {
	projectRoot := t.TempDir()
	decDir := mkdirDec(t, projectRoot)
	writeFile(t, filepath.Join(decDir, "vars.yaml"), "vars:\n  ONLY: main\n")
	// 故意不创建 vars.d/

	mgr := NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadVarsConfig()
	if err != nil {
		t.Fatalf("LoadVarsConfig() 失败: %v (vars.d 缺失应被容忍)", err)
	}
	if cfg.Vars["ONLY"] != "main" {
		t.Fatalf("ONLY = %q, 期望 main", cfg.Vars["ONLY"])
	}
	if len(cfg.Vars) != 1 {
		t.Fatalf("vars 长度 = %d, 期望 1", len(cfg.Vars))
	}
}
