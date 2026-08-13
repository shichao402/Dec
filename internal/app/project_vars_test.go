package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadProjectVarsView_NoVarsFile_NoCache 覆盖空项目场景。
func TestLoadProjectVarsView_NoVarsFile_NoCache(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()

	view, err := LoadProjectVarsView(projectRoot)
	if err != nil {
		t.Fatalf("LoadProjectVarsView() 失败: %v", err)
	}
	if view.VarsFileReady {
		t.Fatal("无 .dec/vars.yaml 时 VarsFileReady 应为 false")
	}
	if view.VarsPath == "" {
		t.Fatal("VarsPath 应返回默认路径，即使文件不存在")
	}
	if len(view.ProjectVars) != 0 {
		t.Fatalf("ProjectVars = %#v, 期望空", view.ProjectVars)
	}
	if len(view.UsedPlaceholders) != 0 {
		t.Fatalf("UsedPlaceholders = %#v, 期望空", view.UsedPlaceholders)
	}
	if view.CacheExists {
		t.Fatal("无 .dec/cache 时 CacheExists 应为 false")
	}
}

// TestLoadProjectVarsView_WithFileAndCache 覆盖项目变量 + cache 占位符的典型场景。
func TestLoadProjectVarsView_WithFileAndCache(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()

	// 写 .dec/vars.yaml
	decDir := filepath.Join(projectRoot, ".dec")
	if err := os.MkdirAll(decDir, 0755); err != nil {
		t.Fatalf("MkdirAll(.dec) 失败: %v", err)
	}
	varsContent := `vars:
  FOO: "foo-from-project"
  BAR: "bar-from-project"
`
	if err := os.WriteFile(filepath.Join(decDir, "vars.yaml"), []byte(varsContent), 0644); err != nil {
		t.Fatalf("写入 vars.yaml 失败: %v", err)
	}

	// 写 .dec/cache 占位符
	cacheDir := filepath.Join(decDir, "cache", "skills", "demo")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll(cache) 失败: %v", err)
	}
	sample := "use {{FOO}} and {{MISSING}} and {{BAR}}"
	if err := os.WriteFile(filepath.Join(cacheDir, "README.md"), []byte(sample), 0644); err != nil {
		t.Fatalf("写入 cache 文件失败: %v", err)
	}

	view, err := LoadProjectVarsView(projectRoot)
	if err != nil {
		t.Fatalf("LoadProjectVarsView() 失败: %v", err)
	}
	if !view.VarsFileReady {
		t.Fatal("期望 VarsFileReady = true")
	}
	if !view.CacheExists {
		t.Fatal("期望 CacheExists = true")
	}
	if view.ProjectVars["FOO"] != "foo-from-project" {
		t.Fatalf("ProjectVars[FOO] = %q, 期望 foo-from-project", view.ProjectVars["FOO"])
	}
	wantUsed := map[string]bool{"FOO": true, "BAR": true, "MISSING": true}
	if len(view.UsedPlaceholders) != len(wantUsed) {
		t.Fatalf("UsedPlaceholders = %#v, 期望 3 项", view.UsedPlaceholders)
	}
	for _, name := range view.UsedPlaceholders {
		if !wantUsed[name] {
			t.Fatalf("UsedPlaceholders 包含意外项 %q", name)
		}
	}
	if status, ok := view.ResolvedVars["FOO"]; !ok || status.Source != PlaceholderSourceProject || status.Value != "foo-from-project" {
		t.Fatalf("ResolvedVars[FOO] = %+v, 期望 project=foo-from-project", status)
	}
	if status, ok := view.ResolvedVars["BAR"]; !ok || status.Source != PlaceholderSourceProject {
		t.Fatalf("ResolvedVars[BAR] = %+v, 期望 project", status)
	}
	if status, ok := view.ResolvedVars["MISSING"]; !ok || status.Source != PlaceholderSourceMissing {
		t.Fatalf("ResolvedVars[MISSING] = %+v, 期望 missing", status)
	}
	missing := view.MissingPlaceholders()
	if len(missing) != 1 || missing[0] != "MISSING" {
		t.Fatalf("MissingPlaceholders() = %#v, 期望 [MISSING]", missing)
	}
}

// TestLoadProjectVarsView_GlobalFallback 覆盖 global 变量回落。
func TestLoadProjectVarsView_GlobalFallback(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	projectRoot := t.TempDir()

	// 写全局 vars（~/.dec/local/vars.yaml）
	globalDir := filepath.Join(decHome, "local")
	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatalf("MkdirAll(~/.dec/local) 失败: %v", err)
	}
	globalVars := `vars:
  GLOBAL_TOKEN: "gv"
`
	if err := os.WriteFile(filepath.Join(globalDir, "vars.yaml"), []byte(globalVars), 0644); err != nil {
		t.Fatalf("写入全局 vars.yaml 失败: %v", err)
	}

	// cache 使用 GLOBAL_TOKEN 占位符，无项目级覆盖
	cacheDir := filepath.Join(projectRoot, ".dec", "cache", "mcp", "demo")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll(cache) 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "config.yaml"), []byte("token: {{GLOBAL_TOKEN}}"), 0644); err != nil {
		t.Fatalf("写入 cache 失败: %v", err)
	}

	view, err := LoadProjectVarsView(projectRoot)
	if err != nil {
		t.Fatalf("LoadProjectVarsView() 失败: %v", err)
	}
	status, ok := view.ResolvedVars["GLOBAL_TOKEN"]
	if !ok {
		t.Fatalf("ResolvedVars 未收录 GLOBAL_TOKEN")
	}
	if status.Source != PlaceholderSourceGlobal {
		t.Fatalf("ResolvedVars[GLOBAL_TOKEN].Source = %q, 期望 global", status.Source)
	}
	if status.Value != "gv" {
		t.Fatalf("ResolvedVars[GLOBAL_TOKEN].Value = %q, 期望 gv", status.Value)
	}
}

// TestEnsureProjectVarsFile_CreateThenIdempotent 覆盖首次创建 + 再次调用不覆盖。
func TestEnsureProjectVarsFile_CreateThenIdempotent(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()

	r1, err := EnsureProjectVarsFile(projectRoot)
	if err != nil {
		t.Fatalf("第一次 EnsureProjectVarsFile() 失败: %v", err)
	}
	if !r1.Created {
		t.Fatal("首次调用应创建模板文件")
	}
	if _, err := os.Stat(r1.Path); err != nil {
		t.Fatalf("stat %s 失败: %v", r1.Path, err)
	}

	// 用户手动修改文件内容
	custom := []byte("vars:\n  CUSTOM: \"1\"\n")
	if err := os.WriteFile(r1.Path, custom, 0644); err != nil {
		t.Fatalf("覆盖写入失败: %v", err)
	}

	r2, err := EnsureProjectVarsFile(projectRoot)
	if err != nil {
		t.Fatalf("第二次 EnsureProjectVarsFile() 失败: %v", err)
	}
	if r2.Created {
		t.Fatal("文件已存在时不应再次创建")
	}
	got, err := os.ReadFile(r2.Path)
	if err != nil {
		t.Fatalf("读取已有文件失败: %v", err)
	}
	if string(got) != string(custom) {
		t.Fatal("EnsureProjectVarsFile 二次调用不应覆盖已有内容")
	}
}
