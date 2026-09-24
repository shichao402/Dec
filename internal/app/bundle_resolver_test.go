package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/types"
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

// setupRepoWithVault 创建临时 repo 目录，并写入 pmodel 项目树。
// files 的 key 相对于 repoDir，例如 "vikunja/public/project/skills/foo/SKILL.md"。
// 对缺少 dec.yaml 的顶层项目目录会自动补一份，避免 Scan 因未声明目录失败。
func setupRepoWithVault(t *testing.T, files map[string]string) string {
	t.Helper()
	repoDir := t.TempDir()
	ensured := map[string]bool{}
	for rel, content := range files {
		writeFile(t, filepath.Join(repoDir, rel), content)
		top := strings.Split(filepath.ToSlash(rel), "/")[0]
		if top != "" && top != "." {
			ensured[top] = ensured[top] || strings.HasSuffix(filepath.ToSlash(rel), "/dec.yaml") || filepath.ToSlash(rel) == top+"/dec.yaml"
		}
	}
	for top, hasManifest := range ensured {
		if hasManifest || top == "projects" || top == "bundles" || strings.HasPrefix(top, ".") {
			continue
		}
		writeFile(t, filepath.Join(repoDir, top, "dec.yaml"), "name: "+top+"\n")
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
		"vikunja/public/project/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"cli/public/project/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
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

func TestResolveDesiredAssetsFiltersWorkspacePlane(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"tools/dec.yaml": "name: tools\n",
		"tools/public/project/skills/project-skill/SKILL.md": "---\nname: project-skill\n---\n",
		"tools/public/user/skills/user-skill/SKILL.md":       "---\nname: user-skill\n---\n",
	})
	cfg := &types.ProjectConfig{ProjectName: "tools"}

	project, err := resolveDesiredAssetsForPlane(cfg, repoDir, WorkspaceProject, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Assets) != 1 || project.Assets[0].Name != "project-skill" {
		t.Fatalf("project 平面应只装 local 资产: %#v", project.Assets)
	}

	user, err := resolveDesiredAssetsForPlane(cfg, repoDir, WorkspaceUser, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(user.Assets) != 0 {
		t.Fatalf("Global 平面不从私仓安装: %#v", user.Assets)
	}
}

func TestResolveDesiredAssets_BundleExpandsMembers(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"combo/public/project/skills/foo/SKILL.md": "---\nname: foo\n---\n",
		"combo/public/project/rules/bar.mdc":       "rule bar\n",
		"combo/dec.yaml":                           "name: combo\n",
	})
	cfg := &types.ProjectConfig{ProjectName: "combo"}

	got, err := resolveDesiredAssets(cfg, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	if len(got.Assets) != 2 {
		t.Fatalf("Assets len = %d, 期望 2; 内容: %#v", len(got.Assets), got.Assets)
	}

	for _, a := range got.Assets {
		key := assetKey(a)
		sources := got.Sources[key]
		if len(sources) != 1 || sources[0] != "p/combo" {
			t.Fatalf("Sources[%s] = %#v, 期望 [p/combo]", key, sources)
		}
	}

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
		t.Fatalf("Bundles = %#v, 期望仅 1 个启用的项目", got.Bundles)
	}
}

func TestResolveDesiredAssetsOfficialRequireWinsOverVaultDependency(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"app/dec.yaml":    "name: app\ndepends_on: [relkit]\n",
		"relkit/dec.yaml": "name: relkit\n",
		"relkit/public/local/skills/relkit-ops/SKILL.md": "---\nname: relkit-ops\n---\nold vault copy\n",
	})
	cfg := &types.ProjectConfig{
		ProjectName: "app",
		Requires:    types.RequiresSpec{"relkit": types.RequiresLatest},
	}

	got, err := resolveDesiredAssetsForPlane(cfg, repoDir, WorkspaceProject, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Assets) != 0 {
		t.Fatalf("官方 requires 不应再从私仓 depends_on 落地同名项目: %#v", got.Assets)
	}
	for _, bundle := range got.Bundles {
		if bundle.Name == "relkit" && bundle.Enabled {
			t.Fatalf("官方 requires 的同名私仓项目不应标为 Enabled: %#v", bundle)
		}
	}
}

func TestResolveDesiredAssetsVaultPinDoesNotInstall(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"app/dec.yaml":    "name: app\ndepends_on: [relkit]\n",
		"relkit/dec.yaml": "name: relkit\n",
		"relkit/public/local/skills/relkit-ops/SKILL.md": "---\nname: relkit-ops\n---\n",
	})
	cfg := &types.ProjectConfig{
		ProjectName: "app",
		Requires:    types.RequiresSpec{"relkit": types.RequiresVault},
	}

	got, err := resolveDesiredAssetsForPlane(cfg, repoDir, WorkspaceProject, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Assets) != 0 {
		t.Fatalf("vault pin 不再从私仓安装: %#v", got.Assets)
	}
}

func TestResolveDesiredAssets_MissingSubscribedProjectWarns(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"combo/public/project/skills/foo/SKILL.md": "---\nname: foo\n---\n",
		"combo/dec.yaml": "name: combo\n",
	})
	cfg := &types.ProjectConfig{
		Requires: types.RequiresSpec{"combo": types.RequiresVault, "ghost": types.RequiresVault},
	}

	var events []OperationEvent
	got, err := resolveDesiredAssets(cfg, repoDir, captureEvents(&events))
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	if len(got.Assets) != 0 {
		t.Fatalf("vault pin 不应安装私仓正文: %#v", got.Assets)
	}
	if len(got.MissingProjects) != 0 {
		t.Fatalf("MissingProjects = %#v, vault pin 不再报缺失", got.MissingProjects)
	}
}

func TestResolveDesiredAssets_UnknownBundleWarns(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"default/public/project/skills/foo/SKILL.md": "---\nname: foo\n---\n",
	})
	cfg := &types.ProjectConfig{
		Requires: types.RequiresSpec{"does-not-exist": types.RequiresVault},
	}

	got, err := resolveDesiredAssets(cfg, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	if len(got.Assets) != 0 {
		t.Fatalf("Assets = %#v, 期望为空", got.Assets)
	}
}

func TestResolveDesiredAssets_MultipleProjectsUniqueAssets(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"a/public/project/skills/only-a/SKILL.md": "---\nname: only-a\n---\n",
		"a/dec.yaml": "name: a\n",
		"b/public/project/skills/only-b/SKILL.md": "---\nname: only-b\n---\n",
		"b/dec.yaml": "name: b\n",
	})
	cfg := &types.ProjectConfig{
		ProjectName: "a",
		Requires:    types.RequiresSpec{"b": types.RequiresVault},
	}

	got, err := resolveDesiredAssets(cfg, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	if len(got.Assets) != 1 || got.Assets[0].Name != "only-a" {
		t.Fatalf("只应安装本仓项目，Assets = %#v", got.Assets)
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
		".git/config":   "",
		".dec/whatever": "",
		"combo/public/project/skills/foo/SKILL.md": "---\nname: foo\n---\n",
		"combo/dec.yaml": "name: combo\n",
	})
	cfg := &types.ProjectConfig{Requires: types.RequiresSpec{"combo": types.RequiresVault}}

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

func TestResolvePAssetsProjectUsesHomeAndDirectPublicRequires(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"my-app/dec.yaml": "name: my-app\nrequires: [shared]\n",
		"my-app/public/project/rules/home-public.mdc":        "home",
		"my-app/private/project/rules/home-private.mdc":      "private",
		"my-app/public/user/rules/home-user.mdc":             "user",
		"shared/dec.yaml":                                    "name: shared\nrequires: [transitive]\n",
		"shared/public/project/skills/shared/SKILL.md":       "shared",
		"shared/private/project/rules/not-visible.mdc":       "private",
		"transitive/dec.yaml":                                "name: transitive\n",
		"transitive/public/project/rules/not-transitive.mdc": "no",
	})
	cfg := &types.ProjectConfig{ProjectName: "my-app"}

	got, err := resolveDesiredAssetsForPlane(cfg, repoDir, WorkspaceProject, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, asset := range got.Assets {
		names[asset.Name] = true
	}
	for _, want := range []string{"home-public", "home-private"} {
		if !names[want] {
			t.Fatalf("缺少 %s: %#v", want, got.Assets)
		}
	}
	for _, gone := range []string{"shared", "not-transitive"} {
		if names[gone] {
			t.Fatalf("depends_on 不应再从私仓安装 %s: %#v", gone, got.Assets)
		}
	}
	for _, forbidden := range []string{"home-user", "not-visible"} {
		if names[forbidden] {
			t.Fatalf("不应解析 %s: %#v", forbidden, got.Assets)
		}
	}
}

func TestResolvePAssetsUserUsesBothUserQuadrants(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"tools/dec.yaml":                         "name: tools\n",
		"tools/public/user/rules/public.mdc":     "public",
		"tools/private/user/rules/private.mdc":   "private",
		"tools/public/project/rules/project.mdc": "project",
	})
	cfg := &types.ProjectConfig{Requires: types.RequiresSpec{"tools": types.RequiresVault}}
	got, err := resolveDesiredAssetsForPlane(cfg, repoDir, WorkspaceUser, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Assets) != 0 {
		t.Fatalf("Global 平面不从私仓安装: %#v", got.Assets)
	}
}

func TestResolvePAssetsRejectsCrossProjectTargetCollision(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"my-app/dec.yaml":                   "name: my-app\nrequires: [a, b]\n",
		"a/dec.yaml":                        "name: a\n",
		"a/public/project/rules/shared.mdc": "a",
		"b/dec.yaml":                        "name: b\n",
		"b/public/project/rules/shared.mdc": "b",
	})
	got, err := resolveDesiredAssetsForPlane(&types.ProjectConfig{ProjectName: "my-app"}, repoDir, WorkspaceProject, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Assets) != 0 {
		t.Fatalf("本仓项目的 depends_on 不应把别的项目装进来: %#v", got.Assets)
	}
}

func TestPAssetPathsIncludeQuadrantAndRequiresAreReadOnlyOnPush(t *testing.T) {
	workspace := NewWorkspace(WorkspaceProject, t.TempDir())
	home := types.TypedAssetRef{
		Type:       "rule",
		Visibility: types.AssetVisibilityPrivate,
		Plane:      types.AssetPlaneProject,
		AssetRef:   types.AssetRef{Name: "home", Vault: "my-app"},
	}
	required := types.TypedAssetRef{
		Type:       "rule",
		Visibility: types.AssetVisibilityPublic,
		Plane:      types.AssetPlaneProject,
		AssetRef:   types.AssetRef{Name: "shared", Vault: "shared-tools"},
	}
	source := filepath.ToSlash(resolveTypedAssetFile("repo", home))
	if !strings.HasSuffix(source, "my-app/private/local/rules/home.mdc") {
		t.Fatalf("source = %q", source)
	}
	cache := filepath.ToSlash(getWorkspaceTypedCachePath(workspace, home))
	if !strings.Contains(cache, ".dec/cache/my-app/private/local/rules/home.mdc") {
		t.Fatalf("cache = %q", cache)
	}
	writable := writableResolvedAssets(workspace, &types.ProjectConfig{ProjectName: "my-app"}, []types.TypedAssetRef{home, required})
	if len(writable) != 1 || writable[0].Vault != "my-app" {
		t.Fatalf("writable = %#v", writable)
	}
}
