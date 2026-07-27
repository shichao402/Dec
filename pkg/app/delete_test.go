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

	// 不查远端时只有 Dec 资产：落地路径散在项目根，扫目录认不出哪些文件是 secret。
	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, false, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	var hasDec bool
	for _, c := range candidates {
		if c.Kind == DeleteKindDecAsset && c.Name == "demo" {
			hasDec = true
		}
		if c.Kind == DeleteKindSecret {
			t.Fatalf("未查远端时不应产出 secret 候选项: %#v", c)
		}
	}
	if !hasDec {
		t.Fatalf("应列出 cache 中的 Dec 资产: %#v", candidates)
	}
}

// 远端有 note、本地也有文件 → 正常候选项，不标 Orphan。
func TestListDeleteCandidates_MarksLocallyPresentSecretAsNotOrphan(t *testing.T) {
	setupSecretsConfigForPushTest(t)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"vikunja_workflow": {{RelativePath: ".config/mise/conf.d/vikunja.toml", Content: "[env]\n"}},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}
	writeProjectFileForPushTest(t, projectRoot, ".config/mise/conf.d/vikunja.toml", "[env]\n")

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, true, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	for _, c := range candidates {
		if c.Kind != DeleteKindSecret {
			continue
		}
		if c.SecretPath != ".config/mise/conf.d/vikunja.toml" {
			t.Fatalf("SecretPath = %q, 期望即 note 名", c.SecretPath)
		}
		if c.Orphan {
			t.Fatalf("本地存在的 secret 不应标 Orphan: %#v", c)
		}
		if c.TreeRoot != secretsTreeRoot || c.TreeBranch != "vikunja_workflow" {
			t.Fatalf("分组 = %q/%q, 期望按 Bitwarden folder 分组", c.TreeRoot, c.TreeBranch)
		}
		return
	}
	t.Fatalf("应列出 secret 候选项: %#v", candidates)
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
					RelativePath: ".config/mise/conf.d/remote-only.toml",
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

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, true, nil)
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

func TestListDeleteCandidates_SkipsRemoteWhenDisabled(t *testing.T) {
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

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		t.Fatal("includeRemote=false 时不应访问 Bitwarden client")
		return nil
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := ListDeleteCandidates(context.Background(), projectRoot, false, nil); err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
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

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, false, nil)
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

func TestListDeleteCandidates_IncludesSSHKeys(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	stub := &secrets.StubClient{
		SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
			"vikunja_workflow": {{Name: "deploy", Hosts: []string{"vikunja.example.com"}, PrivateKey: "priv\n"}},
		},
	}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	// 先落地本地 key，验证非 Orphan。
	landings, err := secrets.PrepareSSHKeyLandings("vikunja", stub.SSHKeysByFolder["vikunja_workflow"])
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.WriteSSHKeyLandings(landings); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{EnabledBundles: []string{"vikunja"}}); err != nil {
		t.Fatal(err)
	}

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, true, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	for _, c := range candidates {
		if c.Kind == DeleteKindSSHKey && c.SSHKeyName == "deploy" {
			if c.Orphan {
				t.Fatalf("本地存在的 SSH Key 不应标 Orphan: %#v", c)
			}
			if c.DecBundleName != "vikunja" || c.SecretsBundle != "vikunja_workflow" {
				t.Fatalf("SSH candidate = %#v", c)
			}
			if !strings.Contains(c.Label, "[ssh] deploy") {
				t.Fatalf("Label = %q", c.Label)
			}
			return
		}
	}
	t.Fatalf("应列出 SSH Key 候选项: %#v", candidates)
}

func TestDeleteProjectItems_RemovesSSHKeyLocalAndRemote(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	stub := &secrets.StubClient{
		SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
			"vikunja_workflow": {{Name: "deploy", Hosts: []string{"vikunja.example.com"}, PrivateKey: "priv\n", PublicKey: "pub\n"}},
		},
	}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	landings, err := secrets.PrepareSSHKeyLandings("vikunja", stub.SSHKeysByFolder["vikunja_workflow"])
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.WriteSSHKeyLandings(landings); err != nil {
		t.Fatal(err)
	}

	result, err := DeleteProjectItems(context.Background(), DeleteProjectInput{
		ProjectRoot: t.TempDir(),
		Confirmed:   true,
		Items: []DeleteSelectionItem{{
			Kind:          DeleteKindSSHKey,
			SSHKeyName:    "deploy",
			DecBundleName: "vikunja",
			SecretsBundle: "vikunja_workflow",
		}},
	}, nil)
	if err != nil {
		t.Fatalf("DeleteProjectItems() = %v", err)
	}
	if result.SSHKeysDeleted != 1 {
		t.Fatalf("SSHKeysDeleted = %d", result.SSHKeysDeleted)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "dec_vikunja_deploy")); !os.IsNotExist(err) {
		t.Fatal("本地私钥应已删除")
	}
	if len(stub.SSHKeysByFolder["vikunja_workflow"]) != 0 {
		t.Fatalf("远端 SSH Key 应已删除: %#v", stub.SSHKeysByFolder["vikunja_workflow"])
	}
}
