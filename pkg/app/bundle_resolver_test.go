package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

// writeFile 在测试目录下写一个文件，自动创建父目录。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) 失败: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) 失败: %v", path, err)
	}
}

// setupRepoWithVault 创建临时 repo 目录，并在给定 vault 下写入若干文件。
// files 的 key 相对于 repoDir，例如 "default/skills/foo/SKILL.md"。
func setupRepoWithVault(t *testing.T, files map[string]string) string {
	t.Helper()
	repoDir := t.TempDir()
	for rel, content := range files {
		writeFile(t, filepath.Join(repoDir, rel), content)
	}
	return repoDir
}

// captureEvents 返回一个 Reporter，把事件收集到给出的切片指针里。
func captureEvents(events *[]OperationEvent) Reporter {
	return ReporterFunc(func(e OperationEvent) {
		*events = append(*events, e)
	})
}

func TestResolveDesiredAssets_StandaloneOnly(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"default/skills/foo/SKILL.md": "---\nname: foo\n---\n",
	})
	cfg := &types.ProjectConfig{
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Name: "foo", Vault: "default"}},
		},
	}

	got, err := resolveDesiredAssets(cfg, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	if len(got.Assets) != 1 || got.Assets[0].Name != "foo" {
		t.Fatalf("Assets = %#v, 期望包含 foo", got.Assets)
	}
	key := assetKey(got.Assets[0])
	if sources := got.Sources[key]; len(sources) != 1 || sources[0] != "standalone" {
		t.Fatalf("Sources[%s] = %#v, 期望 [standalone]", key, sources)
	}
	if len(got.Bundles) != 0 {
		t.Fatalf("Bundles = %#v, 期望为空", got.Bundles)
	}
}

func TestResolveDesiredAssets_BundleExpandsMembers(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"default/skills/foo/SKILL.md": "---\nname: foo\n---\n",
		"default/rules/bar.mdc":       "rule bar\n",
		"default/bundles/combo.yaml": `name: combo
description: combo bundle
members:
  - skill/foo
  - rule/bar
`,
	})
	cfg := &types.ProjectConfig{
		EnabledBundles: []string{"combo"},
	}

	got, err := resolveDesiredAssets(cfg, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	if len(got.Assets) != 2 {
		t.Fatalf("Assets len = %d, 期望 2; 内容: %#v", len(got.Assets), got.Assets)
	}

	// 检查两个成员都被登记为 bundle/combo
	for _, a := range got.Assets {
		key := assetKey(a)
		sources := got.Sources[key]
		if len(sources) != 1 || sources[0] != "bundle/combo" {
			t.Fatalf("Sources[%s] = %#v, 期望 [bundle/combo]", key, sources)
		}
	}

	// bundle 本身被标记为 Enabled
	if len(got.Bundles) != 1 || !got.Bundles[0].Enabled {
		t.Fatalf("Bundles = %#v, 期望 1 个启用的 bundle", got.Bundles)
	}
}

func TestResolveDesiredAssets_BundleMissingMemberWarns(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"default/skills/foo/SKILL.md": "---\nname: foo\n---\n",
		"default/bundles/combo.yaml": `name: combo
members:
  - skill/foo
  - rule/ghost
`,
	})
	cfg := &types.ProjectConfig{
		EnabledBundles: []string{"combo"},
	}

	var events []OperationEvent
	got, err := resolveDesiredAssets(cfg, repoDir, captureEvents(&events))
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}

	// foo 进了目标集，ghost 被跳过
	if len(got.Assets) != 1 || got.Assets[0].Name != "foo" {
		t.Fatalf("Assets = %#v, 期望只有 foo", got.Assets)
	}

	// 应该有针对 ghost 成员的 warning（来自 LoadBundles 的 memberExists 检查）
	// 以及解析阶段对不存在资产文件的兜底 warning
	var sawGhostWarn bool
	for _, e := range events {
		if e.Level == EventWarn && strings.Contains(e.Message, "ghost") {
			sawGhostWarn = true
		}
	}
	if !sawGhostWarn {
		t.Fatalf("期望 ghost 相关 warning，事件: %#v", events)
	}
}

func TestResolveDesiredAssets_BundleAndStandaloneOverlap(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"default/skills/foo/SKILL.md": "---\nname: foo\n---\n",
		"default/bundles/combo.yaml": `name: combo
members:
  - skill/foo
`,
	})
	cfg := &types.ProjectConfig{
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Name: "foo", Vault: "default"}},
		},
		EnabledBundles: []string{"combo"},
	}

	got, err := resolveDesiredAssets(cfg, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}

	if len(got.Assets) != 1 {
		t.Fatalf("Assets len = %d, 期望去重后 1 个", len(got.Assets))
	}

	key := assetKey(got.Assets[0])
	sources := got.Sources[key]
	sort.Strings(sources)
	want := []string{"bundle/combo", "standalone"}
	if len(sources) != 2 || sources[0] != want[0] || sources[1] != want[1] {
		t.Fatalf("Sources[%s] = %#v, 期望 %#v", key, sources, want)
	}
}

func TestResolveDesiredAssets_UnknownBundleWarns(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"default/skills/foo/SKILL.md": "---\nname: foo\n---\n",
	})
	cfg := &types.ProjectConfig{
		EnabledBundles: []string{"does-not-exist"},
	}

	var events []OperationEvent
	got, err := resolveDesiredAssets(cfg, repoDir, captureEvents(&events))
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	if len(got.Assets) != 0 {
		t.Fatalf("Assets = %#v, 期望为空", got.Assets)
	}

	var sawWarn bool
	for _, e := range events {
		if e.Level == EventWarn && strings.Contains(e.Message, "does-not-exist") {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Fatalf("期望 unknown bundle warning，事件: %#v", events)
	}
}

func TestResolveDesiredAssets_MultipleBundlesDedup(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"default/skills/shared/SKILL.md": "---\nname: shared\n---\n",
		"default/skills/onlyA/SKILL.md":  "---\nname: onlyA\n---\n",
		"default/bundles/a.yaml": `name: a
members:
  - skill/shared
  - skill/onlyA
`,
		"default/bundles/b.yaml": `name: b
members:
  - skill/shared
`,
	})
	cfg := &types.ProjectConfig{
		EnabledBundles: []string{"a", "b"},
	}

	got, err := resolveDesiredAssets(cfg, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	if len(got.Assets) != 2 {
		t.Fatalf("Assets len = %d, 期望 2（shared + onlyA）", len(got.Assets))
	}

	// shared 同时来自 bundle/a 和 bundle/b
	var foundShared bool
	for _, a := range got.Assets {
		if a.Name == "shared" {
			foundShared = true
			sources := append([]string(nil), got.Sources[assetKey(a)]...)
			sort.Strings(sources)
			if len(sources) != 2 || sources[0] != "bundle/a" || sources[1] != "bundle/b" {
				t.Fatalf("shared sources = %#v, 期望 [bundle/a bundle/b]", sources)
			}
		}
	}
	if !foundShared {
		t.Fatalf("未在目标集中找到 shared，Assets: %#v", got.Assets)
	}
}

func TestResolveDesiredAssets_NilConfig(t *testing.T) {
	got, err := resolveDesiredAssets(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets(nil) 失败: %v", err)
	}
	if len(got.Assets) != 0 {
		t.Fatalf("Assets = %#v, 期望为空", got.Assets)
	}
}

func TestResolveDesiredAssets_EmptyRepoDir(t *testing.T) {
	cfg := &types.ProjectConfig{}
	got, err := resolveDesiredAssets(cfg, "", nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets(\"\") 失败: %v", err)
	}
	if len(got.Assets) != 0 || len(got.Bundles) != 0 {
		t.Fatalf("结果非空: %#v", got)
	}
}

func TestResolveDesiredAssets_SkipsDotDirs(t *testing.T) {
	// 保证隐藏目录（.git / .dec 等）不会被当作 vault 扫描。
	repoDir := setupRepoWithVault(t, map[string]string{
		".git/config":                    "",
		".dec/whatever":                  "",
		"default/skills/foo/SKILL.md":    "---\nname: foo\n---\n",
		"default/bundles/combo.yaml":     "name: combo\nmembers:\n  - skill/foo\n",
	})
	cfg := &types.ProjectConfig{EnabledBundles: []string{"combo"}}

	got, err := resolveDesiredAssets(cfg, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	// 只应发现 default vault 的 bundle。
	for _, b := range got.Bundles {
		if b.VaultName == ".git" || b.VaultName == ".dec" {
			t.Fatalf("隐藏目录被误当作 vault: %+v", b)
		}
	}
}

func TestAllBundleSourced(t *testing.T) {
	cases := []struct {
		name    string
		sources []string
		want    bool
	}{
		{"empty", nil, false},
		{"only standalone", []string{"standalone"}, false},
		{"mixed", []string{"standalone", "bundle/a"}, false},
		{"only bundle", []string{"bundle/a", "bundle/b"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := allBundleSourced(c.sources); got != c.want {
				t.Fatalf("allBundleSourced(%#v) = %v, 期望 %v", c.sources, got, c.want)
			}
		})
	}
}

func TestAppendUniqueSource(t *testing.T) {
	got := appendUniqueSource([]string{"a", "b"}, "a")
	if len(got) != 2 {
		t.Fatalf("重复添加不应增长: %#v", got)
	}
	got = appendUniqueSource(got, "c")
	if len(got) != 3 || got[2] != "c" {
		t.Fatalf("新来源未追加: %#v", got)
	}
}
