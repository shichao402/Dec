package publish

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/registry"
	"github.com/shichao402/Dec/internal/types"
)

func TestPublishIdempotentAndRejectRewrite(t *testing.T) {
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
	cfg := &types.ProjectConfig{
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
	}
	if err := mgr.SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}

	opts := Options{ProjectRoot: provider, Ref: "v0.1.0", RegistryURL: bare}
	r1, err := Publish(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Tag != "registry/demo/v0.1.0" || r1.Idempotent {
		t.Fatalf("%+v", r1)
	}
	r2, err := Publish(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Idempotent {
		t.Fatal("expected idempotent")
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# demo changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(ctx, opts); err == nil {
		t.Fatal("expected rewrite rejection")
	}
	if err := Yank(ctx, YankOptions{Project: "demo", Ref: "v0.1.0", RegistryURL: bare}); err != nil {
		t.Fatal(err)
	}
	if err := Purge(ctx, PurgeOptions{Project: "demo", Ref: "v0.1.0", Confirm: "registry/demo/v0.1.0", RegistryURL: bare}); err != nil {
		t.Fatal(err)
	}
}
