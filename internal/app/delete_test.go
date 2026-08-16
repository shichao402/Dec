package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/handler"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

func TestPushProjectAssets_PruneDecOrphansWhenCacheRemoved(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/combo/skills/keep-skill/SKILL.md":   "---\nname: keep-skill\n---\nkeep\n",
		"bundles/combo/skills/remove-skill/SKILL.md": "---\nname: remove-skill\n---\nold\n",
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
			Kind:  DeleteKindDecAsset,
			Type:  "skill",
			Name:  "demo",
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

	// 不查远端时仍列 Dec；secrets 只来自本地 SyncTarget 扫描（本用例无 .secrets 文件）。
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
			t.Fatalf("无本地 .secrets 且未查远端时不应产出 secret: %#v", c)
		}
	}
	if !hasDec {
		t.Fatalf("应列出 cache 中的 Dec 资产: %#v", candidates)
	}
}

// 远端有 note、本地也有文件 → 正常候选项，不标 Orphan。
func TestListDeleteCandidates_IncludesLocalSecretsWithoutRemote(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}
	writeProjectFileForPushTest(t, projectRoot, ".secrets/bundles/vikunja/.env/vikunja.env", "TOKEN=1\n")

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, false, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	for _, c := range candidates {
		if c.Kind == DeleteKindSecret && c.SecretPath == ".env/vikunja.env" && !c.Orphan {
			return
		}
	}
	t.Fatalf("应列出本地 secret（无需远端）: %#v", candidates)
}

// 远端有 note、本地也有文件 → 正常候选项，不标 Orphan。
func TestListDeleteCandidates_MarksLocallyPresentSecretAsNotOrphan(t *testing.T) {
	setupSecretsConfigForPushTest(t)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"bundle/vikunja": {{RelativePath: ".env/vikunja.env", Content: "VIKUNJA_API_TOKEN=abc\n"}},
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
	writeProjectFileForPushTest(t, projectRoot, ".secrets/bundles/vikunja/.env/vikunja.env", "VIKUNJA_API_TOKEN=abc\n")

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, true, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	for _, c := range candidates {
		if c.Kind != DeleteKindSecret {
			continue
		}
		if c.SecretPath != ".env/vikunja.env" {
			t.Fatalf("SecretPath = %q, 期望相对同步根的 note 名", c.SecretPath)
		}
		if c.Orphan {
			t.Fatalf("本地存在的 secret 不应标 Orphan: %#v", c)
		}
		if c.TreeRoot != secretsTreeRoot || c.TreeBranch != "bundle/vikunja" {
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
				"bundle/vikunja": {{
					RelativePath: ".env/remote-only.env",
					Content:      "VIKUNJA_API_TOKEN=1\n",
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
		if c.Kind == DeleteKindSecret && strings.Contains(c.SecretPath, "remote-only.env") {
			if !c.Orphan {
				t.Fatalf("远端-only secret 应标记 Orphan: %#v", c)
			}
			if c.TreeBranch != "bundle/vikunja" {
				t.Fatalf("TreeBranch = %q, want vikunja", c.TreeBranch)
			}
			if !strings.Contains(c.Label, "仅远端") {
				t.Fatalf("Label 应含仅远端: %q", c.Label)
			}
			return
		}
	}
	t.Fatalf("应列出远端-only secret: %#v", candidates)
}

// ADR 0004：Remote 应列「包内外」secrets；未启用但 vault 同平面存在的包，远端 Note 仍要出现。
func TestListDeleteCandidates_ListsRemoteSecretsOutsideEnabled(t *testing.T) {
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
				"bundle/vikunja": {{RelativePath: ".env/outside.env", Content: "TOKEN=1\n"}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/bundle.yaml": "name: vikunja\nscope: project\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() = %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "Demo",
		EnabledBundles: nil, // 未启用
	}); err != nil {
		t.Fatal(err)
	}

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, true, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	for _, c := range candidates {
		if c.Kind == DeleteKindSecret && c.SecretPath == ".env/outside.env" {
			if c.SecretsBundle != "bundle/vikunja" {
				t.Fatalf("SecretsBundle = %q, want bundle/vikunja", c.SecretsBundle)
			}
			return
		}
	}
	t.Fatalf("未启用包的远端 secret 也应出现在 Remote: %#v", candidates)
}

// 本地停用后残留的 .secrets/bundles/<name> 仍应出现在 Remote（删除入口）。
func TestListDeleteCandidates_ListsLocalSecretsForDisabledBundle(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: nil,
	}); err != nil {
		t.Fatal(err)
	}
	writeProjectFileForPushTest(t, projectRoot, ".secrets/bundles/vikunja/.env/left.env", "TOKEN=1\n")

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, false, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	for _, c := range candidates {
		if c.Kind == DeleteKindSecret && c.SecretPath == ".env/left.env" {
			return
		}
	}
	t.Fatalf("停用后本地残留 secret 应可在 Remote 列出: %#v", candidates)
}

func TestListDeleteCandidates_IncludesCommandsFromVault(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/pkv/commands/pkv/note.md": "# pkv\n",
		// 无 bundle.yaml：靠 synthesize + listBundleAssetMembers 发现 command
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() = %v", err)
	}
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{}); err != nil {
		t.Fatal(err)
	}

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, false, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	for _, c := range candidates {
		if c.Kind == DeleteKindDecAsset && c.Type == "command" && c.Name == "pkv" && c.Vault == "pkv" {
			return
		}
	}
	t.Fatalf("vault commands 应出现在 Remote: %#v", candidates)
}

// writeRemoteBrowseSecretsConfig 准备一个「已配置 Bitwarden + 有 session」的隔离 DEC_HOME。
func writeRemoteBrowseSecretsConfig(t *testing.T, cfg secrets.Config) {
	t.Helper()
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = "https://vault.example.com"
	}
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
}

func hasSecretCandidate(candidates []DeleteCandidate, folder, notePath string) bool {
	for _, c := range candidates {
		if c.Kind == DeleteKindSecret && c.SecretsBundle == folder && c.SecretPath == notePath {
			return true
		}
	}
	return false
}

// ADR 0004：Bitwarden 上存在、但本机启用列表 / 同步根 / vault 里都没有的 folder
// 属于孤儿，Remote 必须能看见它才能删。浏览候选只从本地名单推导时它永远不可见。
func TestListDeleteCandidates_DiscoversOrphanRemoteFolder(t *testing.T) {
	writeRemoteBrowseSecretsConfig(t, secrets.Config{})

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"bundle/orphan": {{RelativePath: ".env/left-behind.env", Content: "TOKEN=1\n"}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/known/bundle.yaml": "name: known\nscope: project\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() = %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "Demo"}); err != nil {
		t.Fatal(err)
	}

	candidates, err := ListDeleteCandidates(context.Background(), projectRoot, true, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	if !hasSecretCandidate(candidates, "bundle/orphan", ".env/left-behind.env") {
		t.Fatalf("Bitwarden 孤儿 folder 应出现在 Remote: %#v", candidates)
	}
}

// ADR 0004 修订：Remote 全量可见，scope 只作元数据；跨平面 folder 也应出现。
func TestListDeleteCandidates_OrphanDiscoveryCrossPlaneVisible(t *testing.T) {
	writeRemoteBrowseSecretsConfig(t, secrets.Config{})

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"bundle/user-only": {{RelativePath: ".env/machine.env", Content: "TOKEN=1\n"}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/user-only/bundle.yaml": "name: user-only\nscope: user\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() = %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "Demo"}); err != nil {
		t.Fatal(err)
	}

	projectCandidates, err := ListDeleteCandidates(context.Background(), projectRoot, true, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	if !hasSecretCandidate(projectCandidates, "bundle/user-only", ".env/machine.env") {
		t.Fatalf("跨平面 folder 应出现在项目平面 Remote: %#v", projectCandidates)
	}

	userCandidates, err := ListWorkspaceDeleteCandidates(
		context.Background(), NewWorkspace(WorkspaceUser, ""), true, nil)
	if err != nil {
		t.Fatalf("ListWorkspaceDeleteCandidates(user) = %v", err)
	}
	if !hasSecretCandidate(userCandidates, "bundle/user-only", ".env/machine.env") {
		t.Fatalf("scope=user 的 bundle 应出现在用户平面 Remote: %#v", userCandidates)
	}
}

// known_secret_bundles 是本机见过的 secrets 包；Remote 浏览也要拿它当候选，
// 否则「vault 包已删、Bitwarden folder 还在」的残留无从清理。
func TestPlanWorkspaceSecretsBrowse_IncludesKnownSecretBundles(t *testing.T) {
	writeRemoteBrowseSecretsConfig(t, secrets.Config{
		KnownSecretBundles: []string{"remembered"},
	})

	cfg, err := secrets.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planWorkspaceSecretsBrowse(
		NewWorkspace(WorkspaceProject, t.TempDir()), nil, cfg, nil)
	if err != nil {
		t.Fatalf("planWorkspaceSecretsBrowse() = %v", err)
	}
	for _, target := range plan.Targets {
		if target.Folder == "bundle/remembered" {
			return
		}
	}
	t.Fatalf("known_secret_bundles 应进入浏览候选: %#v", plan.Targets)
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
			if c.Partition != PartitionLocal {
				t.Fatalf("本地 cache 资产应属本地分区, got %q", c.Partition)
			}
			if c.TreeRoot != localTreeRootDec || c.TreeBranch != "vikunja" {
				t.Fatalf("Dec 资产 tree = %q/%q, want %s/vikunja", c.TreeRoot, c.TreeBranch, localTreeRootDec)
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
			"bundle/vikunja": {{Name: ".sshkey/deploy", Hosts: []string{"vikunja.example.com"}, PrivateKey: "priv\n"}},
		},
	}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	// 先落地本地 key，验证非 Orphan。
	landings, err := secrets.PrepareSSHKeyLandings("vikunja", stub.SSHKeysByFolder["bundle/vikunja"])
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
		if c.Kind == DeleteKindSSHKey && c.SSHKeyName == ".sshkey/deploy" {
			if c.Orphan {
				t.Fatalf("本地存在的 SSH Key 不应标 Orphan: %#v", c)
			}
			if c.DecBundleName != "vikunja" || c.SecretsBundle != "bundle/vikunja" {
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

func TestDeleteProjectItems_RemovesSSHKeyRemoteOnlyLeavesLocal(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	stub := &secrets.StubClient{
		SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
			"bundle/vikunja": {{Name: ".sshkey/deploy", Hosts: []string{"vikunja.example.com"}, PrivateKey: "priv\n", PublicKey: "pub\n"}},
		},
	}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	landings, err := secrets.PrepareSSHKeyLandings("vikunja", stub.SSHKeysByFolder["bundle/vikunja"])
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.WriteSSHKeyLandings(landings); err != nil {
		t.Fatal(err)
	}

	result, err := DeleteProjectItems(context.Background(), DeleteProjectInput{
		ProjectRoot: t.TempDir(),
		Confirmed:   true,
		Mode:        DeleteModeRemote,
		Items: []DeleteSelectionItem{{
			Kind:          DeleteKindSSHKey,
			SSHKeyName:    ".sshkey/deploy",
			DecBundleName: "vikunja",
			SecretsBundle: "bundle/vikunja",
			Partition:     PartitionRemote,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("DeleteProjectItems() = %v", err)
	}
	if result.SSHKeysDeleted != 1 {
		t.Fatalf("SSHKeysDeleted = %d", result.SSHKeysDeleted)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "dec_vikunja_deploy")); err != nil {
		t.Fatal("远端删除不应碰本地私钥")
	}
	if len(stub.SSHKeysByFolder["bundle/vikunja"]) != 0 {
		t.Fatalf("远端 SSH Key 应已删除: %#v", stub.SSHKeysByFolder["bundle/vikunja"])
	}

	result, err = CleanupLocal(context.Background(), DeleteProjectInput{
		ProjectRoot: t.TempDir(),
		Confirmed:   true,
		Items: []DeleteSelectionItem{{
			Kind:          DeleteKindSSHKey,
			SSHKeyName:    ".sshkey/deploy",
			DecBundleName: "vikunja",
			SecretsBundle: "bundle/vikunja",
			Partition:     PartitionLocal,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("CleanupLocal() = %v", err)
	}
	if result.SSHKeysDeleted != 1 {
		t.Fatalf("SSHKeysDeleted = %d", result.SSHKeysDeleted)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "dec_vikunja_deploy")); !os.IsNotExist(err) {
		t.Fatal("本地清理后私钥应已删除")
	}
}

func TestDeleteProjectItems_RevokesGitGCMNote(t *testing.T) {
	setupSecretsConfigForPushTest(t)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"bundle/vikunja": {{RelativePath: ".gcm/cnb.yaml", Content: "\nhost: cnb.cool\nusername: cnb\npassword: tok\n"}},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	var calls [][]string
	reg := handler.NewRegistry()
	reg.Register(handler.NewGitGCMHandler(func(_ context.Context, _ string, args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}))
	restore := handler.SetDefault(reg)
	t.Cleanup(restore)

	projectRoot := t.TempDir()
	writeProjectFileForPushTest(t, projectRoot, ".secrets/bundles/vikunja/.gcm/cnb.yaml",
		"\nhost: cnb.cool\nusername: cnb\npassword: tok\n")

	result, err := DeleteProjectItems(context.Background(), DeleteProjectInput{
		ProjectRoot: projectRoot,
		Confirmed:   true,
		Mode:        DeleteModeRemote,
		Items: []DeleteSelectionItem{{
			Kind:          DeleteKindSecret,
			SecretPath:    ".gcm/cnb.yaml",
			LocalRoot:     ".secrets/bundles/vikunja",
			Plane:         secrets.SyncPlaneProject,
			SecretsBundle: "bundle/vikunja",
			Partition:     PartitionRemote,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("DeleteProjectItems() = %v", err)
	}
	if result.SecretsDeleted != 1 {
		t.Fatalf("SecretsDeleted = %d, want 1", result.SecretsDeleted)
	}

	var sawReject, sawUnset bool
	for _, c := range calls {
		joined := strings.Join(c, " ")
		if joined == "credential reject" {
			sawReject = true
		}
		if joined == "config --global --unset credential.https://cnb.cool.provider" {
			sawUnset = true
		}
	}
	if !sawReject || !sawUnset {
		t.Fatalf("应调用 credential reject 与 --unset provider: %#v", calls)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "bundles", "vikunja", ".gcm/cnb.yaml")); err != nil {
		t.Fatalf("远端删除不应碰本地 gitgcm note, err=%v", err)
	}
}

func TestDeleteProjectItems_LocalOnlyDecAssetAndPruneEmptyBundleDir(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/bundle.yaml": "name: default\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() = %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{EnabledBundles: []string{"default"}}); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(projectRoot, ".dec", "cache", "default", "skills", "helloworld")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := DeleteProjectItems(context.Background(), DeleteProjectInput{
		ProjectRoot: projectRoot,
		Confirmed:   true,
		Mode:        DeleteModeLocal,
		Items: []DeleteSelectionItem{{
			Kind:      DeleteKindDecAsset,
			Type:      "skill",
			Name:      "helloworld",
			Vault:     "default",
			Partition: PartitionLocal,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("DeleteProjectItems() = %v", err)
	}
	if result.DecDeleted != 1 {
		t.Fatalf("DecDeleted = %d, want 1", result.DecDeleted)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".dec", "cache", "default")); !os.IsNotExist(err) {
		t.Fatalf("空的 .dec/cache/default 应被删掉, err=%v", err)
	}
}

func TestDeleteProjectItems_ProjectPlaneRejectsEmptyRoot(t *testing.T) {
	_, err := DeleteProjectItems(context.Background(), DeleteProjectInput{
		ProjectRoot: "",
		Plane:       WorkspaceProject,
		Confirmed:   true,
		Items: []DeleteSelectionItem{{
			Kind:       DeleteKindSecret,
			SecretPath: ".env/app.env",
		}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "项目根目录不能为空") {
		t.Fatalf("项目平面空根应报错, got %v", err)
	}
}

// 用户平面 Remote 删除不依赖 projectRoot（ADR 0009）：本地 secrets、Dec cache、SSH 均走机器路径。
func TestDeleteProjectItems_UserPlaneEmptyRootDeletesLocalSecretAndRemote(t *testing.T) {
	decHome := setupSecretsConfigForPushTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	localNote := filepath.Join(decHome, "secrets", "bundles", "woa", ".env", "app.env")
	if err := os.MkdirAll(filepath.Dir(localNote), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localNote, []byte("TOKEN=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stub := &secrets.StubClient{
		NotesByFolder: map[string][]secrets.SecureNote{
			"bundle/woa": {{RelativePath: ".env/app.env", Content: "TOKEN=1\n"}},
		},
	}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	result, err := DeleteProjectItems(context.Background(), DeleteProjectInput{
		ProjectRoot: "",
		Plane:       WorkspaceUser,
		Confirmed:   true,
		Mode:        DeleteModeRemote,
		Items: []DeleteSelectionItem{{
			Kind:          DeleteKindSecret,
			SecretPath:    ".env/app.env",
			LocalRoot:     "bundles/woa",
			Plane:         secrets.SyncPlaneMachine,
			SecretsBundle: "bundle/woa",
			Partition:     PartitionRemote,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("DeleteProjectItems(user) = %v", err)
	}
	if result.SecretsDeleted != 1 {
		t.Fatalf("SecretsDeleted = %d, want 1", result.SecretsDeleted)
	}
	if _, err := os.Stat(localNote); err != nil {
		t.Fatalf("远端删除不应碰本地文件, err=%v", err)
	}
	if len(stub.NotesByFolder["bundle/woa"]) != 0 {
		t.Fatalf("远端 Note 应已删除: %#v", stub.NotesByFolder["bundle/woa"])
	}
	if _, err := CleanupLocal(context.Background(), DeleteProjectInput{
		ProjectRoot: "",
		Plane:       WorkspaceUser,
		Confirmed:   true,
		Items: []DeleteSelectionItem{{
			Kind:          DeleteKindSecret,
			SecretPath:    ".env/app.env",
			LocalRoot:     "bundles/woa",
			Plane:         secrets.SyncPlaneMachine,
			SecretsBundle: "bundle/woa",
			Partition:     PartitionLocal,
		}},
	}, nil); err != nil {
		t.Fatalf("CleanupLocal = %v", err)
	}
	if _, err := os.Stat(localNote); !os.IsNotExist(err) {
		t.Fatalf("本地清理后文件应已删除, err=%v", err)
	}
}

func TestDeleteProjectItems_UserPlaneEmptyRootDeletesDecCache(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/woa/bundle.yaml": "name: woa\nscope: user\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() = %v", err)
	}

	skillDir := filepath.Join(decHome, "cache", "woa", "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := DeleteProjectItems(context.Background(), DeleteProjectInput{
		ProjectRoot: "",
		Plane:       WorkspaceUser,
		Confirmed:   true,
		Mode:        DeleteModeLocal,
		Items: []DeleteSelectionItem{{
			Kind:      DeleteKindDecAsset,
			Type:      "skill",
			Name:      "demo",
			Vault:     "woa",
			Partition: PartitionLocal,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("DeleteProjectItems(user dec) = %v", err)
	}
	if result.DecDeleted != 1 {
		t.Fatalf("DecDeleted = %d, want 1", result.DecDeleted)
	}
	if _, err := os.Stat(filepath.Join(decHome, "cache", "woa")); !os.IsNotExist(err) {
		t.Fatalf("空的 ~/.dec/cache/woa 应被删掉, err=%v", err)
	}
}

func TestDeleteProjectItems_UserPlaneEmptyRootDeletesSSHKey(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	stub := &secrets.StubClient{
		SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
			"bundle/woa": {{Name: ".sshkey/deploy", Hosts: []string{"woa.example.com"}, PrivateKey: "priv\n", PublicKey: "pub\n"}},
		},
	}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	landings, err := secrets.PrepareSSHKeyLandings("woa", stub.SSHKeysByFolder["bundle/woa"])
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.WriteSSHKeyLandings(landings); err != nil {
		t.Fatal(err)
	}

	result, err := DeleteProjectItems(context.Background(), DeleteProjectInput{
		ProjectRoot: "",
		Plane:       WorkspaceUser,
		Confirmed:   true,
		Mode:        DeleteModeRemote,
		Items: []DeleteSelectionItem{{
			Kind:          DeleteKindSSHKey,
			SSHKeyName:    ".sshkey/deploy",
			DecBundleName: "woa",
			SecretsBundle: "bundle/woa",
			Partition:     PartitionRemote,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("DeleteProjectItems(user ssh) = %v", err)
	}
	if result.SSHKeysDeleted != 1 {
		t.Fatalf("SSHKeysDeleted = %d, want 1", result.SSHKeysDeleted)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "dec_woa_deploy")); err != nil {
		t.Fatal("远端删除不应碰本地私钥")
	}
	if len(stub.SSHKeysByFolder["bundle/woa"]) != 0 {
		t.Fatalf("远端 SSH Key 应已删除: %#v", stub.SSHKeysByFolder["bundle/woa"])
	}
}

// ADR 0004 修订：Remote 全量可见；合成 scope=project 的包也出现在用户平面库存中。
func TestListDeleteCandidates_SyntheticProjectBundleVisibleOnUserPlane(t *testing.T) {
	writeRemoteBrowseSecretsConfig(t, secrets.Config{})

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"bundle/pkv": {{RelativePath: "pkv.include", Content: "x\n"}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/pkv/commands/pkv/note.md": "# pkv\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() = %v", err)
	}

	userCandidates, err := ListWorkspaceDeleteCandidates(
		context.Background(), NewWorkspace(WorkspaceUser, ""), true, nil)
	if err != nil {
		t.Fatalf("ListWorkspaceDeleteCandidates(user) = %v", err)
	}
	if !hasSecretCandidate(userCandidates, "bundle/pkv", "pkv.include") {
		t.Fatalf("合成 scope=project 的 pkv 应出现在用户平面 Remote: %#v", userCandidates)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "Demo"}); err != nil {
		t.Fatal(err)
	}
	projectCandidates, err := ListDeleteCandidates(context.Background(), projectRoot, true, nil)
	if err != nil {
		t.Fatalf("ListDeleteCandidates() = %v", err)
	}
	if !hasSecretCandidate(projectCandidates, "bundle/pkv", "pkv.include") {
		t.Fatalf("合成 scope=project 的 pkv 应出现在项目平面 Remote: %#v", projectCandidates)
	}
}
