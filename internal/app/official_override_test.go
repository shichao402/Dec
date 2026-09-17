package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/install"
	"github.com/shichao402/Dec/internal/types"
)

func TestOfficialOverridePreviewAndStorage(t *testing.T) {
	root := t.TempDir()
	cfg := &types.ProjectConfig{
		ProjectName: "consumer",
		Requires:    types.RequiresSpec{"relkit": "latest"},
	}
	if err := config.NewProjectConfigManager(root).SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root, ".dec", "cache")
	skill := filepath.Join(cache, "relkit", "public", "local", "skills", "relkit-ops")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := install.WriteInstalledVersion(cache, "relkit", "v0.4.2"); err != nil {
		t.Fatal(err)
	}

	workspace := NewWorkspace(WorkspaceProject, root)
	state, err := ListOfficialAssetEdits(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Assets) != 1 || state.Assets[0].Name != "relkit-ops" {
		t.Fatalf("assets = %+v", state.Assets)
	}
	edit, err := LoadOfficialAssetEdit(workspace, PreviewOfficialOverrideInput{
		Project: "relkit", Type: "skill", Name: "relkit-ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edit.Files) != 1 || edit.Files[0].Content != "# old\n" {
		t.Fatalf("edit = %+v", edit)
	}
	edit.Files[0].Content = "# new\n"
	preview, err := PreviewOfficialOverride(workspace, PreviewOfficialOverrideInput{
		Project: "relkit", Type: "skill", Name: "relkit-ops", Files: edit.Files,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.Diff, "-# old") || !strings.Contains(preview.Diff, "+# new") {
		t.Fatalf("diff = %s", preview.Diff)
	}

	asset, err := findOfficialCacheAsset(workspace, "relkit", "skill", "relkit-ops")
	if err != nil {
		t.Fatal(err)
	}
	override := overrideSourcePath(workspace, asset)
	if err := writeEditableFiles(override, edit.Files, true); err != nil {
		t.Fatal(err)
	}
	if got := activeOverrideSource(workspace, asset); got != override {
		t.Fatalf("active override = %q, want %q", got, override)
	}
}

func TestOfficialOverrideRejectsTraversal(t *testing.T) {
	for _, path := range []string{"..", "../secret"} {
		err := writeEditableFiles(t.TempDir(), []OfficialAssetFile{{Path: path, Content: "x"}}, true)
		if err == nil {
			t.Fatalf("expected traversal rejection for %q", path)
		}
	}
}

func TestCleanupDoesNotDeleteOfficialLocalPlane(t *testing.T) {
	root := t.TempDir()
	official := filepath.Join(root, ".dec", "cache", "relkit", "public", "local", "skills", "relkit-ops", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(official), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(official, []byte("# official\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := install.WriteInstalledVersion(filepath.Join(root, ".dec", "cache"), "relkit", "v0.4.2"); err != nil {
		t.Fatal(err)
	}

	cleanupRemovedAssets(NewWorkspace(WorkspaceProject, root), nil, nil)
	if _, err := os.Stat(official); err != nil {
		t.Fatalf("official local cache must survive personal asset cleanup: %v", err)
	}
}
