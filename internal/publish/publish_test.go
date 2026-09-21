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

func TestPublishWritesOriginRepo(t *testing.T) {
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
		OriginRepo:   "https://github.com/example/playbook.git",
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
	if _, err := Publish(ctx, Options{ProjectRoot: provider, Ref: "v0.1.0", RegistryURL: bare}); err != nil {
		t.Fatal(err)
	}
	checkout := t.TempDir()
	if _, err := registry.Git(ctx, "", "clone", "--branch", registry.Branch, bare, checkout); err != nil {
		t.Fatal(err)
	}
	meta, err := registry.LoadProviderMeta(filepath.Join(checkout, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if meta.OriginRepo != "https://github.com/example/playbook.git" {
		t.Fatalf("origin = %q", meta.OriginRepo)
	}
}

// registry 分支是累积的：上一版发过、这一版从 provides 里删掉的资产，
// 不能继续跟着后面每个 tag 发下去，否则消费方永远 pull 不掉它。
func TestPublishDropsRemovedProvides(t *testing.T) {
	ctx := context.Background()
	bare := seedRegistry(t, ctx)

	provider := t.TempDir()
	for _, name := range []string{"kept", "dropped"} {
		dir := filepath.Join(provider, "DecAssets", "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	provide := func(name string) types.ProjectProvide {
		return types.ProjectProvide{
			Source:     "DecAssets/skills/" + name,
			Visibility: types.AssetVisibilityPublic,
			Plane:      types.AssetPlaneGlobal,
			Type:       "skill",
			Name:       name,
		}
	}
	mgr := config.NewProjectConfigManager(provider)
	cfg := &types.ProjectConfig{
		ProjectName:  "demo",
		ProvidesRoot: "DecAssets",
		Provides:     map[string]types.ProjectProvide{"kept": provide("kept"), "dropped": provide("dropped")},
	}
	if err := mgr.SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(ctx, Options{ProjectRoot: provider, Ref: "v0.1.0", RegistryURL: bare}); err != nil {
		t.Fatal(err)
	}

	cfg.Provides = map[string]types.ProjectProvide{"kept": provide("kept")}
	if err := mgr.SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(ctx, Options{ProjectRoot: provider, Ref: "v0.1.1", RegistryURL: bare}); err != nil {
		t.Fatal(err)
	}

	checkout := t.TempDir()
	if _, err := registry.Git(ctx, "", "clone", "--branch", registry.Branch, bare, checkout); err != nil {
		t.Fatal(err)
	}
	skills := filepath.Join(checkout, "demo", "public", "global", "skills")
	if _, err := os.Stat(filepath.Join(skills, "kept")); err != nil {
		t.Fatalf("保留的资产不见了: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skills, "dropped")); !os.IsNotExist(err) {
		t.Fatalf("删掉的资产仍在快照里: %v", err)
	}
}

func seedRegistry(t *testing.T, ctx context.Context) string {
	t.Helper()
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
	return bare
}
