package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/types"
)

func TestSuggestProjectProvidesScansProductRoots(t *testing.T) {
	project := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(project, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cnb/skills/migrate-to-cnb/SKILL.md", "# ops\n")
	write("cnb/skills/unclaimed/SKILL.md", "# new\n")
	write("forge-robot/commands/forge-robot/README.md", "# cmd\n")
	write("skills/root-level/SKILL.md", "# 仓库根不是作者源\n")

	cfg := &types.ProjectConfig{
		Products: map[string]types.ProductDecl{
			"cnb": {
				Root: "cnb",
				Provides: map[string]types.ProjectProvide{
					"migrate": {
						Source: "skills/migrate-to-cnb", Visibility: types.AssetVisibilityPublic,
						Plane: types.AssetPlaneGlobal, Type: "skill", Name: "migrate-to-cnb",
					},
				},
			},
			"forge-robot": {Root: "forge-robot"},
		},
	}
	if err := config.NewProjectConfigManager(project).SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}

	state, err := SuggestProjectProvides(project)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]ProvideCandidate{}
	for _, candidate := range state.Candidates {
		found[candidate.Product+"/"+candidate.Source] = candidate
	}
	if got, ok := found["cnb/skills/migrate-to-cnb"]; !ok || !got.Declared || got.DeclaredKey != "migrate" {
		t.Fatalf("已登记候选 = %#v", got)
	}
	if got, ok := found["cnb/skills/unclaimed"]; !ok || got.Declared {
		t.Fatalf("产品 root 下未登记候选 = %#v", got)
	}
	if got, ok := found["forge-robot/commands/forge-robot"]; !ok || got.Target != "forge-robot/public/local/commands/forge-robot" {
		t.Fatalf("forge-robot 候选 = %#v", got)
	}
	if _, ok := found["/skills/root-level"]; ok {
		t.Fatal("多产品仓的仓库根不是作者源")
	}

	// load 侧：多产品仓应返回产品视图而不是空的顶层 provides。
	loaded, err := LoadProjectProvides(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Products) != 2 {
		t.Fatalf("Products = %#v", loaded.Products)
	}
	var cnb *ProductProvidesState
	for i := range loaded.Products {
		if loaded.Products[i].Name == "cnb" {
			cnb = &loaded.Products[i]
		}
	}
	if cnb == nil || cnb.Provides["migrate"].Source != "skills/migrate-to-cnb" {
		t.Fatalf("cnb 产品声明 = %#v", cnb)
	}
	if cnb.Targets["migrate"] != "cnb/public/global/skills/migrate-to-cnb" {
		t.Fatalf("cnb target = %#v", cnb.Targets)
	}
	if len(cnb.AuthorDirs) == 0 || cnb.AuthorDirs[0] != "cnb/skills" {
		t.Fatalf("cnb AuthorDirs = %#v", cnb.AuthorDirs)
	}
}

// 身份型产品（ADR 0034）：provides 为空、只发身份。load 必须原样带回，不能丢。
func TestLoadProjectProvidesKeepsIdentityProducts(t *testing.T) {
	project := t.TempDir()
	cfg := &types.ProjectConfig{
		Products: map[string]types.ProductDecl{
			"github": {Root: "github", Tags: []string{"global"}, SecretsPlane: types.AssetPlaneGlobal},
		},
	}
	if err := config.NewProjectConfigManager(project).SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}
	state, err := LoadProjectProvides(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Products) != 1 || state.Products[0].Name != "github" {
		t.Fatalf("Products = %#v", state.Products)
	}
	github := state.Products[0]
	if !github.IdentityOnly || len(github.Provides) != 0 {
		t.Fatalf("身份型产品 = %#v", github)
	}
	if github.SecretsPlane != types.AssetPlaneGlobal || len(github.Tags) != 1 {
		t.Fatalf("tags/secrets_plane = %#v", github)
	}
}

// Console 的保存链路：产品维度保存应整体替换 products，且不带出互斥校验错误。
func TestSaveProjectProvidesReplacesProducts(t *testing.T) {
	project := t.TempDir()
	if err := config.NewProjectConfigManager(project).SaveProjectConfig(&types.ProjectConfig{}); err != nil {
		t.Fatal(err)
	}
	saved, err := SaveProjectProvides(SaveProjectProvidesInput{
		ProjectRoot: project,
		Products: []ProductProvidesInput{
			{
				Name: "cnb", Root: "cnb",
				Provides: map[string]types.ProjectProvide{
					"migrate": {
						Source: "skills/migrate-to-cnb", Visibility: types.AssetVisibilityPublic,
						Plane: types.AssetPlaneGlobal, Type: "skill", Name: "migrate-to-cnb",
					},
				},
			},
			{Name: "github", Root: "github", Tags: []string{"global"}, SecretsPlane: types.AssetPlaneGlobal, IdentityOnly: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Products) != 2 {
		t.Fatalf("保存后 Products = %#v", saved.Products)
	}
	cfg, err := config.NewProjectConfigManager(project).LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Products) != 2 || cfg.Products["cnb"].Root != "cnb" {
		t.Fatalf("落盘 products = %#v", cfg.Products)
	}
	if cfg.Products["github"].Tags[0] != "global" || cfg.Products["github"].SecretsPlane != types.AssetPlaneGlobal {
		t.Fatalf("身份型产品字段 = %#v", cfg.Products["github"])
	}
	if cfg.ProjectName != "" || cfg.ProvidesRoot != "" || cfg.Provides != nil {
		t.Fatalf("多产品保存不应残留单产品声明: %#v", cfg)
	}
}
