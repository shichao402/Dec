package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspacePlaneAliases(t *testing.T) {
	for input, want := range map[WorkspacePlane]WorkspacePlane{
		"global":  WorkspaceGlobal,
		"user":    WorkspaceGlobal,
		"local":   WorkspaceLocal,
		"project": WorkspaceLocal,
		"":        WorkspaceLocal,
	} {
		if got := NewWorkspace(input, " root ").EffectivePlane(); got != want {
			t.Fatalf("plane %q: got %q want %q", input, got, want)
		}
	}
	if got := NewWorkspace("global", " root ").Root; got != "root" {
		t.Fatalf("root not trimmed: %q", got)
	}
}

func TestScanManagedProjectsFindsInitializedAndSkipsNodeModules(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "apps", "web")
	ignored := filepath.Join(root, "node_modules", "ignored")
	for _, dir := range []string{filepath.Join(project, ".dec"), filepath.Join(ignored, ".dec")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("kind: project\nversion: v2\nproject_name: web\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := ScanManagedProjects(context.Background(), root, 6, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Projects) != 1 || result.Projects[0].Root != project {
		t.Fatalf("unexpected scan result: %#v", result.Projects)
	}
}

func TestBrowseDirectoriesReturnsDirectoriesOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "folder"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	listing, err := BrowseDirectories(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "folder" {
		t.Fatalf("unexpected listing: %#v", listing.Entries)
	}
}
