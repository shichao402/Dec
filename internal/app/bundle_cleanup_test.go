package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

func TestRemoveBundle_PrunesProjectsKnownAndLocalSecrets(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	useStubSecretsSession(t)

	if err := secrets.RememberSecretBundles([]string{"pkv", "woa"}); err != nil {
		t.Fatal(err)
	}
	global := &types.GlobalConfig{EnabledBundles: []string{"pkv", "woa"}}
	if err := config.SaveGlobalConfig(global); err != nil {
		t.Fatal(err)
	}

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/pkv/commands/pkv/note.md": "# pkv\n",
		"projects/Dec.yaml": `name: Dec
bundles:
  - default
  - pkv
`,
		"projects/Other.yaml": `name: Other
bundles:
  - pkv
  - woa
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() = %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"pkv"},
	}); err != nil {
		t.Fatal(err)
	}
	secretDir := filepath.Join(projectRoot, ".secrets", "pkv", "env")
	if err := os.MkdirAll(secretDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretDir, "x.env"), []byte("A=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	machineSecret := filepath.Join(decHome, "secrets", "pkv", "env")
	if err := os.MkdirAll(machineSecret, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(machineSecret, "m.env"), []byte("B=2\n"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := RemoveBundle(RemoveBundleInput{
		ProjectRoot: projectRoot,
		BundleName:  "pkv",
		Confirmed:   true,
	}, nil)
	if err != nil {
		t.Fatalf("RemoveBundle() = %v", err)
	}
	if len(result.PrunedProjects) != 2 {
		t.Fatalf("PrunedProjects = %#v", result.PrunedProjects)
	}

	cfg, err := secrets.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range cfg.KnownSecretBundleNames() {
		if name == "pkv" {
			t.Fatalf("known_secret_bundles 仍含 pkv: %#v", cfg.KnownSecretBundleNames())
		}
	}

	updated, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.EnabledBundles) != 0 {
		t.Fatalf("项目 enabled 应清空 pkv: %#v", updated.EnabledBundles)
	}
	globalAfter, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range globalAfter.EnabledBundles {
		if name == "pkv" {
			t.Fatalf("用户平面 enabled 仍含 pkv: %#v", globalAfter.EnabledBundles)
		}
	}

	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "pkv")); !os.IsNotExist(err) {
		t.Fatalf("项目 secrets 同步根应已删, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(decHome, "secrets", "pkv")); !os.IsNotExist(err) {
		t.Fatalf("机器 secrets 同步根应已删, err=%v", err)
	}

	// vault projects 不再引用 pkv
	if err := withAppReadRepo(func(tx *repo.Transaction) error {
		for _, name := range []string{"Dec", "Other"} {
			proj, ok, loadErr := LoadVaultProject(tx.WorkDir(), name)
			if loadErr != nil || !ok {
				t.Fatalf("LoadVaultProject(%s) ok=%v err=%v", name, ok, loadErr)
			}
			for _, b := range proj.Bundles {
				if b == "pkv" {
					t.Fatalf("projects/%s.yaml 仍含 pkv: %#v", name, proj.Bundles)
				}
			}
		}
		if _, err := os.Stat(filepath.Join(tx.WorkDir(), "bundles", "pkv")); !os.IsNotExist(err) {
			t.Fatalf("bundles/pkv 应已删除")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveAsset_LastMemberClearsBundleAndKnown(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	useStubSecretsSession(t)
	if err := secrets.RememberSecretBundles([]string{"pkv"}); err != nil {
		t.Fatal(err)
	}

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/pkv/commands/pkv/note.md": "# pkv\n",
		"projects/Dec.yaml": `name: Dec
bundles:
  - pkv
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() = %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"pkv"},
	}); err != nil {
		t.Fatal(err)
	}
	writeProjectFileForPushTest(t, projectRoot, ".secrets/pkv/.env/left.env", "X=1\n")

	_, err := RemoveAsset(RemoveAssetInput{
		ProjectRoot: projectRoot,
		Type:        "command",
		Name:        "pkv",
		Vault:       "pkv",
		Confirmed:   true,
	}, nil)
	if err != nil {
		t.Fatalf("RemoveAsset() = %v", err)
	}

	cfg, err := secrets.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.KnownSecretBundleNames()) != 0 {
		t.Fatalf("删空 bundle 后 known 应无 pkv: %#v", cfg.KnownSecretBundleNames())
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "pkv")); !os.IsNotExist(err) {
		t.Fatalf("本地 secrets 应已清")
	}
	if err := withAppReadRepo(func(tx *repo.Transaction) error {
		proj, ok, loadErr := LoadVaultProject(tx.WorkDir(), "Dec")
		if loadErr != nil || !ok {
			t.Fatalf("LoadVaultProject: ok=%v err=%v", ok, loadErr)
		}
		if len(proj.Bundles) != 0 {
			t.Fatalf("projects/Dec.yaml bundles = %#v", proj.Bundles)
		}
		if _, err := os.Stat(filepath.Join(tx.WorkDir(), "bundles", "pkv")); !os.IsNotExist(err) {
			t.Fatalf("空 bundle 目录应删除")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPushAfterRemoveBundle_DoesNotResurrectFromLocalRemnants(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	useStubSecretsSession(t)

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/pkv/commands/pkv/note.md":      "# pkv\n",
		"bundles/default/skills/hello/SKILL.md": "---\nname: hello\n---\n",
		"projects/dec.yaml": `name: dec
bundles:
  - pkv
  - default
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() = %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "dec",
		EnabledBundles: []string{"pkv", "default"},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := RemoveBundle(RemoveBundleInput{
		ProjectRoot: projectRoot,
		BundleName:  "pkv",
		Confirmed:   true,
	}, nil)
	if err != nil {
		t.Fatalf("RemoveBundle() = %v", err)
	}

	// 模拟「旧 bug」残留：本地又出现 secrets / cache，且误把 pkv 写回 enabled。
	writeProjectFileForPushTest(t, projectRoot, ".secrets/pkv/.env/ghost.env", "GHOST=1\n")
	cacheCmd := filepath.Join(projectRoot, ".dec", "cache", "pkv", "commands", "pkv", "note.md")
	if err := os.MkdirAll(filepath.Dir(cacheCmd), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheCmd, []byte("# ghost\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "dec",
		EnabledBundles: []string{"pkv", "default"},
	}); err != nil {
		t.Fatal(err)
	}

	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{}}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	_, err = PushProjectAssets(context.Background(), projectRoot, nil)
	if err == nil || !strings.Contains(err.Error(), "Push 已拒绝") {
		t.Fatalf("旧结构 Push 应拒绝并引导迁移，err=%v", err)
	}

	// Dec：vault 已无 pkv 声明 → resolve 忽略 enabled 中的 pkv，不应把 cache 写回 vault。
	if err := withAppReadRepo(func(tx *repo.Transaction) error {
		if _, err := os.Stat(filepath.Join(tx.WorkDir(), "bundles", "pkv")); !os.IsNotExist(err) {
			t.Fatalf("push 不应复活 bundles/pkv")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Secrets：enabled 仍含 pkv 时 push 会扫描本地（设计如此）；本测试断言「删除清理后默认不会留下本地根」。
	// 这里故意种回本地根以验证 Dec 侧不复活；secrets 侧若仍启用则会推 — 收敛删除的关键是清 enabled+本地根。
	// 再跑一次：去掉 enabled 后 secrets 不应再建 folder。
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "dec",
		EnabledBundles: []string{"default"},
	}); err != nil {
		t.Fatal(err)
	}
	stub.NotesByFolder = map[string][]secrets.SecureNote{}
	secResult, err := PushSecretsBundles(context.Background(), projectRoot, nil)
	if err != nil {
		t.Fatalf("PushSecretsBundles() = %v", err)
	}
	if _, ok := stub.NotesByFolder["pkv/private/project"]; ok {
		t.Fatalf("未启用时不应 push 出 pkv: %#v", stub.NotesByFolder)
	}
	if secResult.CreatedCount != 0 {
		t.Fatalf("未启用 pkv 时不应新建 secrets: %#v", secResult)
	}
}

func TestDiscoverRemoteSecretTargets_DoesNotRememberOrphans(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	useStubSecretsSession(t)

	cfgPath := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(cfgPath, 0755); err != nil {
		t.Fatal(err)
	}
	data, _ := yaml.Marshal(secrets.Config{ServerURL: "https://vault.example.com"})
	if err := os.WriteFile(filepath.Join(cfgPath, "config.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}

	client := &secrets.StubClient{
		NotesByFolder: map[string][]secrets.SecureNote{
			"bundle/pkv": {{RelativePath: ".env/x.env", Content: "X=1\n"}},
		},
	}
	// StubClient.ListSecretBundleNames 从 NotesByFolder 推导
	workspace := NewWorkspace(WorkspaceProject, t.TempDir())
	extra := discoverRemoteSecretTargets(context.Background(), client, workspace, vaultBundleScopes{
		inPlane:    map[string]struct{}{},
		otherPlane: map[string]struct{}{},
	}, nil, nil)
	if len(extra) != 1 {
		t.Fatalf("应发现孤儿 folder: %#v", extra)
	}
	cfg, err := secrets.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.KnownSecretBundleNames()) != 0 {
		t.Fatalf("浏览孤儿不应写入 known_secret_bundles: %#v", cfg.KnownSecretBundleNames())
	}
}
