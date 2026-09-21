package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

func TestCreateLocalAssetWritesSkillTemplate(t *testing.T) {
	root := t.TempDir()
	res, err := CreateLocalAsset(CreateLocalAssetInput{
		Workspace:  NewWorkspace(WorkspaceLocal, root),
		Project:    "demo",
		Kind:       "skill",
		Name:       "hello",
		Visibility: types.AssetVisibilityPrivate,
		Plane:      types.AssetPlaneLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, ".dec", "cache", "demo", "private", "local", "skills", "hello", "SKILL.md")
	if res.Path != want {
		t.Fatalf("path=%s want=%s", res.Path, want)
	}
}

func TestCreateLocalAssetWritesProvidesForHomeProject(t *testing.T) {
	root := t.TempDir()
	mgr := config.NewProjectConfigManager(root)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:  "playbook",
		ProvidesRoot: "DecAssets",
		Provides: map[string]types.ProjectProvide{
			"seed": {
				Source:     "DecAssets/skills/seed",
				Visibility: types.AssetVisibilityPublic,
				Plane:      types.AssetPlaneGlobal,
				Type:       "skill",
				Name:       "seed",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := CreateLocalAsset(CreateLocalAssetInput{
		Workspace:  NewWorkspace(WorkspaceLocal, root),
		Project:    "playbook",
		Kind:       "skill",
		Name:       "hello",
		Visibility: types.AssetVisibilityPublic,
		Plane:      types.AssetPlaneGlobal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "provides" {
		t.Fatalf("mode=%s", res.Mode)
	}
	want := filepath.Join(root, "DecAssets", "skills", "hello", "SKILL.md")
	if res.Path != want {
		t.Fatalf("path=%s want=%s", res.Path, want)
	}
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := cfg.Provides["skill-hello"]
	if !ok || got.Source != "DecAssets/skills/hello" {
		t.Fatalf("provides=%#v", cfg.Provides)
	}
}

func TestCreateLocalAssetOfficialRequireWritesDraft(t *testing.T) {
	root := t.TempDir()
	mgr := config.NewProjectConfigManager(root)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName: "my-app",
		Requires:    types.RequiresSpec{"playbook": types.RequiresLatest},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := CreateLocalAsset(CreateLocalAssetInput{
		Workspace: NewWorkspace(WorkspaceLocal, root),
		Project:   "playbook",
		Kind:      "skill",
		Name:      "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "draft" {
		t.Fatalf("mode=%s path=%s", res.Mode, res.Path)
	}
}

func TestOfficialGitPushBlockedWhenProvidesDeclared(t *testing.T) {
	if officialGitPushBlocked(&types.ProjectConfig{Provides: map[string]types.ProjectProvide{"x": {}}}) == "" {
		t.Fatal("should block")
	}
	if officialGitPushBlocked(&types.ProjectConfig{}) != "" {
		t.Fatal("empty provides should allow personal push")
	}
}

func TestCreateLocalAssetDirectManagedWritesProvides(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	playbook := t.TempDir()
	mgr := config.NewProjectConfigManager(playbook)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:  "agent-dev-playbook",
		ProvidesRoot: "DecAssets",
		Provides: map[string]types.ProjectProvide{
			"skill-seed": {
				Source:     "DecAssets/skills/seed",
				Visibility: types.AssetVisibilityPublic,
				Plane:      types.AssetPlaneGlobal,
				Type:       "skill",
				Name:       "seed",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"agent-dev-playbook/dec.yaml": "name: agent-dev-playbook\naccess: direct\ntags: [global]\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{
		RepoURL:         remote,
		ManagedProjects: []types.ManagedProject{{Root: playbook}},
	}); err != nil {
		t.Fatal(err)
	}

	consumer := t.TempDir()
	res, err := CreateLocalAsset(CreateLocalAssetInput{
		Workspace:  NewWorkspace(WorkspaceLocal, consumer),
		Project:    "agent-dev-playbook",
		Kind:       "skill",
		Name:       "extracted",
		Visibility: types.AssetVisibilityPublic,
		Plane:      types.AssetPlaneGlobal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "provides" {
		t.Fatalf("mode=%s path=%s", res.Mode, res.Path)
	}
	want := filepath.Join(playbook, "DecAssets", "skills", "extracted", "SKILL.md")
	if res.Path != want {
		t.Fatalf("path=%s want=%s", res.Path, want)
	}
	cache := filepath.Join(consumer, ".dec", "cache")
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("direct write must not create consumer cache: %v", err)
	}
}

func TestFilterOfficialVaultAssetsDropsRegistryPins(t *testing.T) {
	req := types.RequiresSpec{"playbook": types.RequiresLatest, "notes": types.RequiresVault}
	assets := []types.TypedAssetRef{
		{AssetRef: types.AssetRef{Vault: "playbook", Name: "a"}},
		{AssetRef: types.AssetRef{Vault: "notes", Name: "b"}},
	}
	got := filterOfficialVaultAssets(req, assets)
	if len(got) != 1 || got[0].Vault != "notes" {
		t.Fatalf("%#v", got)
	}
}
