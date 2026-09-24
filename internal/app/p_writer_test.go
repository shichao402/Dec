package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

func TestPWriterSetRequiresWritesConsumerConfig(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"my-app/dec.yaml": "name: my-app\n",
		"shared/dec.yaml": "name: shared\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	pinRegistryToEmptyRemote(t, remote)
	root := t.TempDir()
	mgr := config.NewProjectConfigManager(root)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "my-app"}); err != nil {
		t.Fatal(err)
	}

	result, err := DefaultPWriter().SetRequires(
		context.Background(),
		NewWorkspace(WorkspaceProject, root),
		types.RequiresSpec{"shared": types.RequiresLatest, "my-app": types.RequiresVault},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.HomeProject != "my-app" {
		t.Fatalf("HomeProject = %q, 期望 my-app", result.HomeProject)
	}
	if len(result.Subscribed) != 1 || result.Subscribed[0].Project != "shared" || result.Subscribed[0].Pin != types.RequiresLatest {
		t.Fatalf("Subscribed = %#v, 期望仅 shared:latest", result.Subscribed)
	}
	if len(result.Rejected) != 1 || !strings.Contains(result.Rejected[0], "my-app") {
		t.Fatalf("本仓项目应被拒: %#v", result.Rejected)
	}
	if err := withAppReadRepo(func(tx *repo.Transaction) error {
		loaded, err := pmodel.Load(tx.WorkDir(), "my-app")
		if err != nil {
			return err
		}
		if len(loaded.Manifest.DependsOn) != 0 {
			t.Fatalf("订阅不得改写提供方 depends_on: %#v", loaded.Manifest.DependsOn)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Requires["shared"] != types.RequiresLatest || len(cfg.Requires) != 1 {
		t.Fatalf("Requires = %#v, 期望 {shared: latest}", cfg.Requires)
	}
}

func TestPWriterSetRequiresUserWritesGlobalConfig(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"tools/dec.yaml": "name: tools\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	pinRegistryToEmptyRemote(t, remote)
	result, err := DefaultPWriter().SetRequires(
		context.Background(),
		NewWorkspace(WorkspaceUser, ""),
		types.RequiresSpec{"tools": types.RequiresLatest},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Subscribed) != 1 || result.Subscribed[0].Project != "tools" {
		t.Fatalf("result = %#v", result)
	}
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Requires["tools"] != types.RequiresLatest || len(cfg.Requires) != 1 {
		t.Fatalf("Requires = %#v", cfg.Requires)
	}
}

func TestPushAfterRemovePDoesNotResurrectLocalRemnants(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"my-app/dec.yaml":                      "name: my-app\n",
		"my-app/private/local/rules/local.mdc": "remote\n",
		"shared/dec.yaml":                      "name: shared\n",
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

	ghost := filepath.Join(root, ".dec", "cache", "my-app", "private", "local", "rules", "local.mdc")
	if err := os.MkdirAll(filepath.Dir(ghost), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ghost, []byte("ghost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 改绑到仍存在的 shared：已删除项目的 cache 残留不可写，push 不得把它复活。
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "shared"}); err != nil {
		t.Fatal(err)
	}
	if _, err := PushProjectAssets(context.Background(), root, nil); err != nil {
		t.Fatal(err)
	}
	if err := withAppReadRepo(func(tx *repo.Transaction) error {
		if _, err := os.Stat(filepath.Join(tx.WorkDir(), "my-app")); !os.IsNotExist(err) {
			t.Fatalf("push 不应从非绑定项目的本地残留复活: %v", err)
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
			t.Fatalf("Remote 不应展示项目的 Git 四象限资产: %#v", item)
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
		t.Fatalf("depends_on 副本只读，不应进入 push 预览: %#v", preview)
	}
}
