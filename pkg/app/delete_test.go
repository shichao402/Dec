package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
	"gopkg.in/yaml.v3"
)

func TestPushProjectAssets_PruneDecOrphansWhenCacheRemoved(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/combo/skills/keep-skill/SKILL.md":    "---\nname: keep-skill\n---\nkeep\n",
		"bundles/combo/skills/remove-skill/SKILL.md":  "---\nname: remove-skill\n---\nold\n",
		"bundles/combo/bundle.yaml": `name: combo
members:
  - skill/keep-skill
  - skill/remove-skill
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatal(err)
	}

	keepSkill := filepath.Join(projectRoot, ".dec", "cache", "combo", "skills", "keep-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(keepSkill), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepSkill, []byte("---\nname: keep-skill\n---\nkeep\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := PushProjectAssets(context.Background(), projectRoot, nil)
	if err != nil {
		t.Fatalf("PushProjectAssets() = %v", err)
	}
	if result.DecPushedCount == 0 {
		t.Fatalf("DecPushedCount = 0, want >0")
	}

	tx, err := repo.NewReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	removedPath := filepath.Join(tx.WorkDir(), "bundles/combo/skills/remove-skill/SKILL.md")
	if _, err := os.Stat(removedPath); !os.IsNotExist(err) {
		t.Fatalf("远端 remove-skill 应被删除, stat err = %v", err)
	}
	keepPath := filepath.Join(tx.WorkDir(), "bundles/combo/skills/keep-skill/SKILL.md")
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("远端 keep-skill 应保留: %v", err)
	}
}

func TestDeleteProjectItems_RejectsUnconfirmed(t *testing.T) {
	_, err := DeleteProjectItems(context.Background(), DeleteProjectInput{
		ProjectRoot: t.TempDir(),
		Items: []DeleteSelectionItem{{
			Kind: DeleteKindDecAsset,
			Type: "skill",
			Name: "demo",
			Vault: "default",
		}},
	}, nil)
	if err != ErrDeleteNotConfirmed {
		t.Fatalf("err = %v, want ErrDeleteNotConfirmed", err)
	}
}

func TestListDeleteCandidates_IncludesCacheAndSecrets(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}

	skillPath := filepath.Join(projectRoot, ".dec", "cache", "vikunja", "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("---\nname: demo\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(projectRoot, ".secrets", "vikunja_workflow", "mise", "conf.d", "vikunja.toml")
	if err := os.MkdirAll(filepath.Dir(secretPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("[env]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	var hasDec, hasSecret bool
	for _, c := range candidates {
		if c.Kind == DeleteKindDecAsset && c.Name == "demo" {
			hasDec = true
		}
		if c.Kind == DeleteKindSecret && strings.Contains(c.SecretPath, "vikunja.toml") {
			hasSecret = true
		}
	}
	if !hasDec {
		t.Fatalf("应列出 cache 中的 Dec 资产: %#v", candidates)
	}
	if !hasSecret {
		t.Fatalf("应列出 .secrets 文件: %#v", candidates)
	}
}

func TestListDeleteCandidates_IncludesSecretsWhenBindingDiffersFromDir(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}

	// 无 secrets 绑定时 ResolveBinding("vikunja") 为 "vikunja"，但文件落在 vikunja_workflow。
	secretPath := filepath.Join(projectRoot, ".secrets", "vikunja_workflow", "mise", "conf.d", "vikunja.toml")
	if err := os.MkdirAll(filepath.Dir(secretPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("[env]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	for _, c := range candidates {
		if c.Kind == DeleteKindSecret && strings.Contains(c.SecretPath, "vikunja.toml") {
			if c.TreeBranch != "vikunja_workflow" {
				t.Fatalf("secret TreeBranch = %q, want vikunja_workflow", c.TreeBranch)
			}
			return
		}
	}
	t.Fatalf("绑定名与目录不一致时仍应列出 secret: %#v", candidates)
}

func TestListDeleteCandidates_IncludesRemoteOnlySecrets(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := secrets.Config{ServerURL: "https://vault.example.com"}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}

	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"vikunja_workflow": {{
					RelativePath: "mise/conf.d/remote-only.toml",
					Content:      "[env]\nTOKEN=1\n",
				}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	for _, c := range candidates {
		if c.Kind == DeleteKindSecret && strings.Contains(c.SecretPath, "remote-only.toml") {
			if !c.Orphan {
				t.Fatalf("远端-only secret 应标记 Orphan: %#v", c)
			}
			if c.TreeBranch != "vikunja_workflow" {
				t.Fatalf("TreeBranch = %q, want vikunja_workflow", c.TreeBranch)
			}
			if !strings.Contains(c.Label, "仅远端") {
				t.Fatalf("Label 应含仅远端: %q", c.Label)
			}
			return
		}
	}
	t.Fatalf("应列出远端-only secret: %#v", candidates)
}

func TestListDeleteCandidates_GroupsDecAssetsUnderBundle(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}

	skillPath := filepath.Join(projectRoot, ".dec", "cache", "vikunja", "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("---\nname: demo\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	for _, c := range candidates {
		if c.Kind == DeleteKindDecAsset && c.Name == "demo" {
			if c.TreeRoot != ".dec" || c.TreeBranch != "vikunja" {
				t.Fatalf("Dec 资产 tree = %q/%q, want .dec/vikunja", c.TreeRoot, c.TreeBranch)
			}
			return
		}
	}
	t.Fatalf("应列出 cache 中的 Dec 资产: %#v", candidates)
}

func TestDeleteGroupContext_GroupTitle(t *testing.T) {
	ctx := &deleteGroupContext{projectName: "Dec"}
	if got := ctx.groupTitle(secrets.ProjectSecretsDecBundleName); got != "Dec (project)" {
		t.Fatalf("groupTitle(project) = %q, want Dec (project)", got)
	}
	if got := ctx.groupTitle("vikunja"); got != "vikunja (bundle)" {
		t.Fatalf("groupTitle(bundle) = %q, want vikunja (bundle)", got)
	}
}

func TestDeleteGroupContext_ProjectOrderFirst(t *testing.T) {
	ctx := &deleteGroupContext{
		bundleOrder: map[string]int{"vikunja": 0, "default": 1},
	}
	if order := ctx.orderFor(secrets.ProjectSecretsDecBundleName); order != -1 {
		t.Fatalf("project order = %d, want -1", order)
	}
	if order := ctx.orderFor("vikunja"); order != 0 {
		t.Fatalf("vikunja order = %d, want 0", order)
	}
}
