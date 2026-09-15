package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/types"
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

func TestScanManagedProjectsFindsInitializedAndGitProjectsAndSkipsNodeModules(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "apps", "web")
	gitProject := filepath.Join(root, "services", "api")
	ignored := filepath.Join(root, "node_modules", "ignored")
	for _, dir := range []string{filepath.Join(project, ".dec"), filepath.Join(ignored, ".dec")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("kind: project\nversion: v2\nproject_name: web\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(gitProject, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	result, err := ScanManagedProjects(context.Background(), root, 6, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Projects) != 2 ||
		result.Projects[0].Root != project ||
		result.Projects[1].Root != gitProject ||
		!result.Projects[0].Initialized ||
		result.Projects[1].Initialized {
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

func TestProjectConsumersOnlyReturnsDirectManagedReferences(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	root := t.TempDir()
	writeProject := func(dir, home string) ManagedProjectState {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, ".dec"), 0o755); err != nil {
			t.Fatal(err)
		}
		data := []byte("kind: project\nversion: v2\nproject_name: " + home + "\n")
		if err := os.WriteFile(filepath.Join(dir, ".dec", "config.yaml"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		return ManagedProjectState{Root: dir, Name: home, Exists: true, Initialized: true}
	}
	direct := writeProject(filepath.Join(root, "direct"), "app")
	unrelated := writeProject(filepath.Join(root, "unrelated"), "other")
	broken := ManagedProjectState{Root: filepath.Join(root, "broken"), Exists: true, Initialized: true, Error: "invalid"}
	projects := map[string]*pmodel.Loaded{
		"app":    {Manifest: types.P{Name: "app", Requires: []string{"shared"}}},
		"other":  {Manifest: types.P{Name: "other"}},
		"shared": {Manifest: types.P{Name: "shared"}},
	}

	got := projectConsumers("shared", []ManagedProjectState{unrelated, broken, direct}, projects)
	if len(got.Consumers) != 1 || got.Consumers[0].Root != direct.Root {
		t.Fatalf("consumers = %#v", got.Consumers)
	}
}
