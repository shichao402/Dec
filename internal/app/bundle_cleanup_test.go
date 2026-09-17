package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/pmodel"
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
	global := &types.GlobalConfig{Requires: types.RequiresSpec{"pkv": types.RequiresVault, "woa": types.RequiresVault}}
	if err := config.SaveGlobalConfig(global); err != nil {
		t.Fatal(err)
	}

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"pkv/public/project/commands/pkv/note.md": "# pkv\n",
		"pkv/dec.yaml":                            "name: pkv\n",
		"dec/dec.yaml":                            "name: dec\ndepends_on: [default, pkv]\n",
		"other/dec.yaml":                          "name: other\ndepends_on: [pkv, woa]\n",
		"default/dec.yaml":                        "name: default\n",
		"woa/dec.yaml":                            "name: woa\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() = %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		Requires: types.RequiresSpec{"pkv": types.RequiresVault},
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

	result, err := DefaultPWriter().RemoveBundle(RemoveBundleInput{
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
	if len(updated.Requires.VaultProjects()) != 0 {
		t.Fatalf("项目 enabled 应清空 pkv: %#v", updated.Requires.VaultProjects())
	}
	globalAfter, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range globalAfter.Requires.VaultProjects() {
		if name == "pkv" {
			t.Fatalf("用户平面 enabled 仍含 pkv: %#v", globalAfter.Requires.VaultProjects())
		}
	}

	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "pkv")); !os.IsNotExist(err) {
		t.Fatalf("项目 secrets 同步根应已删, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(decHome, "secrets", "pkv")); !os.IsNotExist(err) {
		t.Fatalf("机器 secrets 同步根应已删, err=%v", err)
	}

	// depends_on 与项目目录不再引用 pkv
	if err := withAppReadRepo(func(tx *repo.Transaction) error {
		for _, name := range []string{"dec", "other"} {
			loaded, loadErr := pmodel.Load(tx.WorkDir(), name)
			if loadErr != nil {
				t.Fatalf("Load(%s) = %v", name, loadErr)
			}
			if containsName(loaded.Manifest.DependsOn, "pkv") {
				t.Fatalf("%s depends_on 仍含 pkv: %#v", name, loaded.Manifest.DependsOn)
			}
		}
		if _, err := os.Stat(filepath.Join(tx.WorkDir(), "pkv")); !os.IsNotExist(err) {
			t.Fatalf("pkv 项目目录应已删除")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
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
