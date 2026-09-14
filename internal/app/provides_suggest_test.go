package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/types"
)

func TestSuggestProjectProvidesFindsAuthorSources(t *testing.T) {
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
	write("skills/relkit-ops/SKILL.md", "# ops\n")
	write(".cursor/skills/house-helper/SKILL.md", "# helper\n")
	write("rules/house-style.mdc", "rule\n")
	write(".secrets/relkit/.env/upload.env", "TOKEN=x\n")
	write(".dec/cache/relkit/public/local/skills/installed/SKILL.md", "# 安装产物\n")

	cfg := &types.ProjectConfig{
		ProjectName: "relkit",
		Provides: map[string]types.ProjectProvide{
			"ops": {
				Source: "skills/relkit-ops", Visibility: types.AssetVisibilityPublic,
				Plane: types.AssetPlaneGlobal, Type: "skill", Name: "relkit-ops",
			},
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
		found[candidate.Source] = candidate
	}

	if helper, ok := found[".cursor/skills/house-helper"]; ok {
		t.Fatalf("IDE 渲染产物不应作为 provides 候选: %#v", helper)
	}
	rule, ok := found["rules/house-style.mdc"]
	if !ok || rule.Type != "rule" || rule.Name != "house-style" {
		t.Fatalf("rule 候选 = %#v", rule)
	}
	// secrets 由 SyncTarget 规则覆盖；出现在候选里就等于请人写第二套规则。
	if secret, ok := found[".secrets/relkit/.env/upload.env"]; ok {
		t.Fatalf("secret 不应作为 provides 候选: %#v", secret)
	}
	if _, ok := found[".dec/cache/relkit/public/local/skills/installed"]; ok {
		t.Fatal("Dec 状态目录不能被当成作者源候选")
	}
	// 已登记的仍要出现，但必须标记出来，避免重复添加同一个 source。
	ops, ok := found["skills/relkit-ops"]
	if !ok || !ops.Declared || ops.DeclaredKey != "ops" {
		t.Fatalf("已登记候选 = %#v", ops)
	}
}

// 配了作者根之后，扫描必须整体挪过去：仓库根下的同名目录不再是作者源。
func TestSuggestProjectProvidesFollowsProvidesRoot(t *testing.T) {
	project := t.TempDir()
	write := func(rel string) {
		path := filepath.Join(project, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("dec-assets/skills/relkit-ops/SKILL.md")
	write("skills/business-rules/SKILL.md") // 同名业务目录，不该被认领

	cfg := &types.ProjectConfig{ProjectName: "relkit", ProvidesRoot: "dec-assets"}
	if err := config.NewProjectConfigManager(project).SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}

	state, err := SuggestProjectProvides(project)
	if err != nil {
		t.Fatal(err)
	}
	if state.ProvidesRoot != "dec-assets" {
		t.Fatalf("ProvidesRoot = %q", state.ProvidesRoot)
	}
	sources := map[string]bool{}
	for _, candidate := range state.Candidates {
		sources[candidate.Source] = true
	}
	if !sources["dec-assets/skills/relkit-ops"] {
		t.Fatalf("作者根下的资产未被扫到: %#v", state.Candidates)
	}
	if sources["skills/business-rules"] {
		t.Fatal("作者根之外的同名目录不应被认领")
	}
}
