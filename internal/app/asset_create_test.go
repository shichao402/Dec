package app

import (
	"os"
	"path/filepath"
	"testing"

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
