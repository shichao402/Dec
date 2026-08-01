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

// setupRepoWithVault 创建临时 repo 目录，并在 bundles/ 下写入若干文件。
// files 的 key 相对于 repoDir，例如 "bundles/default/skills/foo/SKILL.md"。
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

func TestResolveDesiredAssets_NilConfigScansBundles(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
	})

	got, err := resolveDesiredAssets(nil, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets(nil) 失败: %v", err)
	}
	if len(got.Assets) != 0 {
		t.Fatalf("nil config 时不应解析 Assets, got %#v", got.Assets)
	}
	if len(got.Bundles) < 2 {
		t.Fatalf("nil config 时仍应扫描 Bundles, got %d", len(got.Bundles))
	}
}

func TestResolveDesiredAssets_BundleExpandsMembers(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"bundles/combo/skills/foo/SKILL.md": "---\nname: foo\n---\n",
		"bundles/combo/rules/bar.mdc":       "rule bar\n",
		"bundles/combo/bundle.yaml": `name: combo
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

	// combo 启用；default 无资产故不合成
	if len(got.Bundles) != 1 {
		t.Fatalf("Bundles len = %d, 期望 1（combo）", len(got.Bundles))
	}
	enabledCount := 0
	for _, b := range got.Bundles {
		if b.Enabled {
			enabledCount++
		}
	}
	if enabledCount != 1 {
		t.Fatalf("Bundles = %#v, 期望仅 1 个启用的 bundle", got.Bundles)
	}
}

func TestResolveDesiredAssets_BundleMissingMemberWarns(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"bundles/combo/skills/foo/SKILL.md": "---\nname: foo\n---\n",
		"bundles/combo/bundle.yaml": `name: combo
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

func TestResolveDesiredAssets_UnknownBundleWarns(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"bundles/default/skills/foo/SKILL.md": "---\nname: foo\n---\n",
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
		"bundles/a/skills/shared/SKILL.md": "---\nname: shared\n---\n",
		"bundles/a/skills/onlyA/SKILL.md":  "---\nname: onlyA\n---\n",
		"bundles/a/bundle.yaml": `name: a
members:
  - skill/shared
  - skill/onlyA
`,
		"bundles/b/skills/shared/SKILL.md": "---\nname: shared\n---\n",
		"bundles/b/bundle.yaml": `name: b
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
	if len(got.Assets) != 3 {
		t.Fatalf("Assets len = %d, 期望 3（shared@a + onlyA@a + shared@b）", len(got.Assets))
	}

	// shared@a 来自 bundle/a，shared@b 来自 bundle/b（不同 bundle 目录下为独立资产）
	var sharedA, sharedB bool
	for _, a := range got.Assets {
		if a.Name != "shared" {
			continue
		}
		sources := append([]string(nil), got.Sources[assetKey(a)]...)
		sort.Strings(sources)
		switch a.Vault {
		case "a":
			sharedA = true
			if len(sources) != 1 || sources[0] != "bundle/a" {
				t.Fatalf("shared@a sources = %#v, 期望 [bundle/a]", sources)
			}
		case "b":
			sharedB = true
			if len(sources) != 1 || sources[0] != "bundle/b" {
				t.Fatalf("shared@b sources = %#v, 期望 [bundle/b]", sources)
			}
		}
	}
	if !sharedA || !sharedB {
		t.Fatalf("未在目标集中找到 shared@a 与 shared@b，Assets: %#v", got.Assets)
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
		"bundles/combo/skills/foo/SKILL.md": "---\nname: foo\n---\n",
		"bundles/combo/bundle.yaml":     "name: combo\nmembers:\n  - skill/foo\n",
	})
	cfg := &types.ProjectConfig{EnabledBundles: []string{"combo"}}

	got, err := resolveDesiredAssets(cfg, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	// 只应发现 combo bundle。
	for _, b := range got.Bundles {
		if b.VaultName == ".git" || b.VaultName == ".dec" {
			t.Fatalf("隐藏目录被误当作 bundle: %+v", b)
		}
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
