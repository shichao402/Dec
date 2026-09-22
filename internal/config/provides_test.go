package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/types"
)

func validProvide(source, name string) types.ProjectProvide {
	return types.ProjectProvide{
		Source: source, Visibility: types.AssetVisibilityPublic,
		Plane: types.AssetPlaneLocal, Type: "skill", Name: name,
	}
}

func TestNormalizeProjectProvides(t *testing.T) {
	got, err := NormalizeProjectProvides("demo", "", map[string]types.ProjectProvide{
		"tool": validProvide(`skills\demo-tool`, "demo-tool"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["tool"].Source != "skills/demo-tool" {
		t.Fatalf("source = %q", got["tool"].Source)
	}
	target, err := ProjectProvideTarget("demo", got["tool"])
	if err != nil {
		t.Fatal(err)
	}
	if target != "demo/public/local/skills/demo-tool" {
		t.Fatalf("target = %q", target)
	}
}

func TestNormalizeProjectProvidesRejectsUnsafeAndNonBijective(t *testing.T) {
	tests := []struct {
		name     string
		provides map[string]types.ProjectProvide
	}{
		{"absolute", map[string]types.ProjectProvide{"a": validProvide(filepath.Join(t.TempDir(), "asset"), "a")}},
		{"parent", map[string]types.ProjectProvide{"a": validProvide("../asset", "a")}},
		{"dec state", map[string]types.ProjectProvide{"a": validProvide(".dec/cache/a", "a")}},
		{"ide output", map[string]types.ProjectProvide{"a": validProvide(".cursor/skills/a", "a")}},
		{"wrong author dir", map[string]types.ProjectProvide{"a": validProvide("assets/a", "a")}},
		{"name mismatch", map[string]types.ProjectProvide{"a": validProvide("skills/a", "b")}},
		{"same target", map[string]types.ProjectProvide{
			"a": validProvide("skills/same", "same"),
			"b": validProvide("skills/same", "same"),
		}},
		{"secret", map[string]types.ProjectProvide{
			"s": {Source: ".secrets/demo/token.env", Visibility: types.AssetVisibilityPrivate, Plane: types.AssetPlaneLocal, Type: "secret", Name: "token"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NormalizeProjectProvides("demo", "", tt.provides); err == nil {
				t.Fatalf("期望拒绝 %#v", tt.provides)
			}
		})
	}
}

func TestProjectConfigProvidesV2RoundTrip(t *testing.T) {
	root := t.TempDir()
	mgr := NewProjectConfigManager(root)
	cfg := &types.ProjectConfig{
		ProjectName: "demo",
		Provides: map[string]types.ProjectProvide{
			"rule": {
				Source: "rules/demo.mdc", Visibility: types.AssetVisibilityPrivate,
				Plane: types.AssetPlaneGlobal, Type: "rule", Name: "demo",
			},
		},
	}
	if err := mgr.SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != types.ProjectConfigVersionV2 || loaded.Provides["rule"].Source != "rules/demo.mdc" {
		t.Fatalf("round trip = %#v", loaded)
	}
}

// 作者根只挪动本地路径，派生的 vault target 必须原样不变——
// 否则换个基准点就等于换了一套远端布局。
func TestProvidesRootMovesSourceNotTarget(t *testing.T) {
	got, err := NormalizeProjectProvides("demo", "dec-assets", map[string]types.ProjectProvide{
		"tool": validProvide("dec-assets/skills/demo-tool", "demo-tool"),
	})
	if err != nil {
		t.Fatal(err)
	}
	target, err := ProjectProvideTarget("demo", got["tool"])
	if err != nil {
		t.Fatal(err)
	}
	if target != "demo/public/local/skills/demo-tool" {
		t.Fatalf("target = %q，作者根不应影响远端布局", target)
	}
	// 设了基准点之后，仓库根下的同名路径不再是合法来源。
	if _, err := NormalizeProjectProvides("demo", "dec-assets", map[string]types.ProjectProvide{
		"tool": validProvide("skills/demo-tool", "demo-tool"),
	}); err == nil {
		t.Fatal("期望拒绝作者根之外的来源")
	}
}

func TestNormalizeProvidesRootRejectsToolNamespaces(t *testing.T) {
	for _, raw := range []string{".dec", ".dec/assets", ".cursor/skills", ".secrets", "../outside", "/abs"} {
		if _, err := NormalizeProvidesRoot(raw); err == nil {
			t.Fatalf("期望拒绝 provides_root %q", raw)
		}
	}
	for raw, want := range map[string]string{"": "", ".": "", "dec-assets/": "dec-assets", `a\b`: "a/b"} {
		got, err := NormalizeProvidesRoot(raw)
		if err != nil || got != want {
			t.Fatalf("NormalizeProvidesRoot(%q) = %q, %v", raw, got, err)
		}
	}
}

func TestResolveProvidesRootDefaultsNewProjectsAndPreservesLegacy(t *testing.T) {
	root, err := ResolveProvidesRoot("", nil)
	if err != nil || root != DefaultProvidesRoot {
		t.Fatalf("new project root = %q, %v", root, err)
	}
	legacy := map[string]types.ProjectProvide{
		"tool": validProvide("skills/demo-tool", "demo-tool"),
	}
	root, err = ResolveProvidesRoot("", legacy)
	if err != nil || root != "" {
		t.Fatalf("legacy project root = %q, %v", root, err)
	}
}

func TestDeclaredProductsKeepSourceRelativeToRoot(t *testing.T) {
	got, err := NormalizeDeclaredProducts(map[string]types.ProductDecl{
		"woa": {
			Root: "woa",
			Provides: map[string]types.ProjectProvide{
				"gongfeng": validProvide("skills/gongfeng", "gongfeng"),
			},
		},
		"tencent-cloud": {
			Root: `tencent-cloud\`,
			Provides: map[string]types.ProjectProvide{
				"tencent-cloud": validProvide("skills/tencent-cloud", "tencent-cloud"),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["woa"].Provides["gongfeng"].Source != "skills/gongfeng" {
		t.Fatalf("source = %q", got["woa"].Provides["gongfeng"].Source)
	}
	if got["tencent-cloud"].Root != "tencent-cloud" {
		t.Fatalf("root = %q", got["tencent-cloud"].Root)
	}
	authors, err := AuthorProducts(&types.ProjectConfig{Products: got})
	if err != nil {
		t.Fatal(err)
	}
	if len(authors) != 2 || authors[0].Name != "tencent-cloud" || authors[1].Name != "woa" {
		t.Fatalf("authors = %#v", authors)
	}
	if authors[1].Provides["gongfeng"].Source != "woa/skills/gongfeng" {
		t.Fatalf("repo source = %q", authors[1].Provides["gongfeng"].Source)
	}
	target, err := ProjectProvideTarget("woa", got["woa"].Provides["gongfeng"])
	if err != nil {
		t.Fatal(err)
	}
	if target != "woa/public/local/skills/gongfeng" {
		t.Fatalf("target = %q", target)
	}
}

func TestDeclaredProductsRejectOverlapAndMixedAuthor(t *testing.T) {
	_, err := NormalizeDeclaredProducts(map[string]types.ProductDecl{
		"woa":        {Root: "woa"},
		"woa-nested": {Root: "woa/extra"},
	})
	if err == nil {
		t.Fatal("期望拒绝重叠的作者目录")
	}
	root := t.TempDir()
	mgr := NewProjectConfigManager(root)
	err = mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName: "demo",
		Products: map[string]types.ProductDecl{
			"woa": {Root: "woa", Provides: map[string]types.ProjectProvide{
				"gongfeng": validProvide("skills/gongfeng", "gongfeng"),
			}},
		},
	})
	if err == nil {
		t.Fatal("期望拒绝 products 与 project_name 同时存在")
	}
}

func TestAuthorProductsFoldsSingleProjectWithoutRewriting(t *testing.T) {
	cfg := &types.ProjectConfig{
		ProjectName:  "relkit",
		ProvidesRoot: "DecAssets",
		Provides: map[string]types.ProjectProvide{
			"relkit-ops": validProvide("DecAssets/skills/relkit-ops", "relkit-ops"),
		},
	}
	authors, err := AuthorProducts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(authors) != 1 || authors[0].Name != "relkit" || authors[0].Root != "DecAssets" {
		t.Fatalf("authors = %#v", authors)
	}
	if authors[0].Provides["relkit-ops"].Source != "DecAssets/skills/relkit-ops" {
		t.Fatalf("source = %q", authors[0].Provides["relkit-ops"].Source)
	}
	if len(cfg.Products) != 0 {
		t.Fatal("单产品仓不应被改写成 products")
	}

	root := t.TempDir()
	mgr := NewProjectConfigManager(root)
	if err := mgr.SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".dec", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "products:") {
		t.Fatalf("单产品配置不应写出 products:\n%s", data)
	}
}

func TestProductsConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	mgr := NewProjectConfigManager(root)
	cfg := &types.ProjectConfig{
		Products: map[string]types.ProductDecl{
			"woa": {
				Root: "woa",
				Provides: map[string]types.ProjectProvide{
					"gongfeng": validProvide("skills/gongfeng", "gongfeng"),
				},
			},
		},
	}
	if err := mgr.SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".dec", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "project_name:") || strings.HasPrefix(line, "provides_root:") {
			t.Fatalf("多产品配置不应写出单产品字段:\n%s", data)
		}
	}
	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Products["woa"].Provides["gongfeng"].Source != "skills/gongfeng" {
		t.Fatalf("round trip = %#v", loaded.Products)
	}
}

// secret 的映射由 SyncTarget 规则唯一决定，provides 不得再写一套（ADR 0026）。
func TestProjectProvideRejectsSecretType(t *testing.T) {
	_, err := ProjectProvideTarget("demo", types.ProjectProvide{
		Visibility: types.AssetVisibilityPrivate,
		Plane:      types.AssetPlaneLocal,
		Type:       "secret",
		Name:       "api-token",
	})
	if err == nil {
		t.Fatal("secret 不应能派生 provides 目标")
	}
}
