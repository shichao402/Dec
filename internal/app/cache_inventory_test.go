package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/types"
)

func TestUnionTypedAssetsIncludesCacheOnlySkill(t *testing.T) {
	base := []types.TypedAssetRef{{
		Type: "rule", Visibility: types.AssetVisibilityPrivate, Plane: types.AssetPlaneLocal,
		AssetRef: types.AssetRef{Name: "old", Vault: "demo"},
	}}
	extra := []types.TypedAssetRef{{
		Type: "skill", Visibility: types.AssetVisibilityPrivate, Plane: types.AssetPlaneLocal,
		AssetRef: types.AssetRef{Name: "new", Vault: "demo"},
	}}
	got := unionTypedAssets(base, extra)
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestScanQuadrantFindsCacheSkill(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo", "private", "local", "skills", "hello")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assets, err := pmodel.ScanQuadrant(root, "demo", types.AssetVisibilityPrivate, types.AssetPlaneLocal)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].Name != "hello" {
		t.Fatalf("%#v", assets)
	}
}
