package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/types"
)

func TestSaveAndLoadProjectConfig(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	cfg := &types.ProjectConfig{
		IDEs:           []string{"cursor"},
		Editor:         "vim",
		EnabledBundles: []string{"vikunja", "cli"},
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

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	if len(loaded.EnabledBundles) != 2 || loaded.EnabledBundles[0] != "vikunja" {
		t.Fatalf("enabled_bundles = %#v, 期望 [vikunja cli]", loaded.EnabledBundles)
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

func TestLoadProjectConfig_FoldsLegacyEnabledIntoBundles(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	decDir := filepath.Join(projectRoot, ".dec")
	if err := os.MkdirAll(decDir, 0755); err != nil {
		t.Fatalf("创建 .dec 目录失败: %v", err)
	}
	configPath := filepath.Join(decDir, "config.yaml")
	legacy := `version: v2
enabled_bundles:
    - tencent-cloud
available:
    vikunja:
        vikunja-workflow:
            skills: true
        vikunja-project-bootstrap:
            skills: true
enabled:
    vikunja:
        vikunja-workflow:
            skills: true
    default:
        helloworld:
            skills: true
`
	if err := os.WriteFile(configPath, []byte(legacy), 0644); err != nil {
		t.Fatalf("写入 legacy 配置失败: %v", err)
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	// enabled 里的 vault 折叠成同名 bundle，已有的 enabled_bundles 保持在前。
	want := []string{"tencent-cloud", "default", "vikunja"}
	if len(loaded.EnabledBundles) != len(want) {
		t.Fatalf("enabled_bundles = %#v, 期望 %#v", loaded.EnabledBundles, want)
	}
	got := map[string]int{}
	for i, name := range loaded.EnabledBundles {
		got[name] = i
	}
	if got["tencent-cloud"] != 0 {
		t.Fatalf("原有 bundle 应排在前面, 实际: %#v", loaded.EnabledBundles)
	}
	for _, name := range want {
		if _, ok := got[name]; !ok {
			t.Fatalf("enabled_bundles 应包含 %q, 实际: %#v", name, loaded.EnabledBundles)
		}
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取回写后的配置失败: %v", err)
	}
	content := string(raw)
	if strings.Contains(content, "available:") || strings.Contains(content, "enabled:\n") {
		t.Fatalf("回写后不应保留 available / enabled 段, 实际内容:\n%s", content)
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
	// v1 的 enabled 只声明了 team vault，available 里的 infra 不代表用户意图，应被丢弃。
	if len(loaded.EnabledBundles) != 1 || loaded.EnabledBundles[0] != "team" {
		t.Fatalf("enabled_bundles = %#v, 期望 [team]", loaded.EnabledBundles)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取迁移后的配置失败: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "version: v2") {
		t.Fatalf("迁移后配置应写入 version: v2, 实际内容:\n%s", content)
	}
	if strings.Contains(content, "- name:") || strings.Contains(content, "available:") {
		t.Fatalf("迁移后不应保留 v1 资产结构, 实际内容:\n%s", content)
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
