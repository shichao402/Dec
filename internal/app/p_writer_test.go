package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

func TestPWriterProjectSelectionWritesHomeRequires(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"my-app/dec.yaml": "name: my-app\n",
		"shared/dec.yaml": "name: shared\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	mgr := config.NewProjectConfigManager(root)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "my-app"}); err != nil {
		t.Fatal(err)
	}

	result, err := DefaultPWriter().SaveProjects(
		NewWorkspace(WorkspaceProject, root), []string{"my-app", "shared"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "p" || result.HomeProject != "my-app" ||
		len(result.RequiredProjects) != 1 || result.RequiredProjects[0] != "shared" {
		t.Fatalf("result = %#v", result)
	}
	if err := withAppReadRepo(func(tx *repo.Transaction) error {
		loaded, err := pmodel.Load(tx.WorkDir(), "my-app")
		if err != nil {
			return err
		}
		if len(loaded.Manifest.Requires) != 1 || loaded.Manifest.Requires[0] != "shared" {
			t.Fatalf("requires = %#v", loaded.Manifest.Requires)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.EnabledBundles) != 0 {
		t.Fatalf("P 模型不得继续把 project requires 写入 enabled_bundles: %#v", cfg.EnabledBundles)
	}
}

func TestPWriterUserSelectionWritesEnabledProjects(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"tools/dec.yaml": "name: tools\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	result, err := DefaultPWriter().SaveProjects(
		NewWorkspace(WorkspaceUser, ""), []string{"tools"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "p" || len(result.EnabledProjects) != 1 {
		t.Fatalf("result = %#v", result)
	}
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.EnabledProjects) != 1 || cfg.EnabledProjects[0] != "tools" {
		t.Fatalf("enabled_projects = %#v", cfg.EnabledProjects)
	}
}

func TestPushAfterRemovePDoesNotResurrectLocalRemnants(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"my-app/dec.yaml":                        "name: my-app\n",
		"my-app/private/project/rules/local.mdc": "remote\n",
		"shared/dec.yaml":                        "name: shared\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	mgr := config.NewProjectConfigManager(root)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "my-app"}); err != nil {
		t.Fatal(err)
	}
	if _, err := DefaultPWriter().RemoveBundle(RemoveBundleInput{
		ProjectRoot: root, BundleName: "my-app", Confirmed: true,
	}, nil); err != nil {
		t.Fatal(err)
	}

	ghost := filepath.Join(root, ".dec", "cache", "my-app", "private", "project", "rules", "local.mdc")
	if err := os.MkdirAll(filepath.Dir(ghost), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ghost, []byte("ghost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "my-app"}); err != nil {
		t.Fatal(err)
	}
	if _, err := PushProjectAssets(context.Background(), root, nil); err != nil {
		t.Fatal(err)
	}
	if err := withAppReadRepo(func(tx *repo.Transaction) error {
		if _, err := os.Stat(filepath.Join(tx.WorkDir(), "my-app")); !os.IsNotExist(err) {
			t.Fatalf("push 不应从本地残留复活 P: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRemotePModelDoesNotExposeGitQuadrants(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"my-app/dec.yaml":                      "name: my-app\n",
		"my-app/public/project/rules/git.mdc":  "public\n",
		"my-app/private/project/rules/git.mdc": "private\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := config.NewProjectConfigManager(root).SaveProjectConfig(
		&types.ProjectConfig{ProjectName: "my-app"}); err != nil {
		t.Fatal(err)
	}
	items, err := ListRemoteInventory(context.Background(),
		NewWorkspace(WorkspaceProject, root), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Kind == DeleteKindDecAsset {
			t.Fatalf("Remote 不应展示 P 的 Git 四象限资产: %#v", item)
		}
	}
}

func TestPreviewPushPDoesNotCountRequiredProjectCopies(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"my-app/dec.yaml":                      "name: my-app\nrequires: [shared]\n",
		"shared/dec.yaml":                      "name: shared\n",
		"shared/public/project/rules/base.mdc": "remote\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := config.NewProjectConfigManager(root).SaveProjectConfig(
		&types.ProjectConfig{ProjectName: "my-app"}); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root, ".dec", "cache", "shared", "public", "project", "rules", "base.mdc")
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte("local change\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewPushProjectAssets(root)
	if err != nil {
		t.Fatal(err)
	}
	if preview.DecHasChanges || preview.DecCandidateCount != 0 {
		t.Fatalf("requires 副本只读，不应进入 push 预览: %#v", preview)
	}
}
