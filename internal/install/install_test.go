package install

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/publish"
	"github.com/shichao402/Dec/internal/registry"
	"github.com/shichao402/Dec/internal/types"
)

func TestOfficialInstallsPublishedTag(t *testing.T) {
	ctx := context.Background()
	bare := t.TempDir()
	if _, err := registry.Git(ctx, "", "init", "--bare", bare); err != nil {
		t.Fatal(err)
	}
	seed := t.TempDir()
	if _, err := registry.Git(ctx, "", "clone", bare, seed); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Git(ctx, seed, "checkout", "--orphan", registry.Branch); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "README"), []byte("registry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Git(ctx, seed, "config", "user.email", "t@t"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Git(ctx, seed, "config", "user.name", "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Git(ctx, seed, "add", "README"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Git(ctx, seed, "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Git(ctx, seed, "push", "-u", "origin", registry.Branch); err != nil {
		t.Fatal(err)
	}

	provider := t.TempDir()
	skillDir := filepath.Join(provider, "DecAssets", "skills", "demo-ops")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := config.NewProjectConfigManager(provider)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:  "demo",
		ProvidesRoot: "DecAssets",
		Provides: map[string]types.ProjectProvide{
			"demo-ops": {
				Source:     "DecAssets/skills/demo-ops",
				Visibility: types.AssetVisibilityPublic,
				Plane:      types.AssetPlaneLocal,
				Type:       "skill",
				Name:       "demo-ops",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := publish.Publish(ctx, publish.Options{ProjectRoot: provider, Ref: "v0.1.0", RegistryURL: bare}); err != nil {
		t.Fatal(err)
	}

	cache := t.TempDir()
	got, err := Official(ctx, Options{
		CacheDir: cache, RegistryURL: bare,
		Requires: types.RequiresSpec{"demo": "v0.1.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Tag != "registry/demo/v0.1.0" {
		t.Fatalf("%+v", got)
	}
	assets, err := ListCache(cache, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].Name != "demo-ops" {
		t.Fatalf("%+v", assets)
	}
	if got := ReadInstalledVersion(cache, "demo"); got != "v0.1.0" {
		t.Fatalf("installed version = %q", got)
	}
	status, err := Status(ctx, Options{
		CacheDir: cache, RegistryURL: bare,
		Requires: types.RequiresSpec{"demo": "latest"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 1 || status[0].Available != "v0.1.0" || status[0].UpdateAvailable {
		t.Fatalf("status = %+v", status)
	}
	if _, err := Official(ctx, Options{
		CacheDir: cache, RegistryURL: bare,
		Requires: types.RequiresSpec{"demo": "v9.9.9"},
	}); err == nil {
		t.Fatal("missing version should fail")
	}
}
