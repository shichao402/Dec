package pmodel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/types"
)

func put(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFourQuadrants(t *testing.T) {
	root := t.TempDir()
	put(t, root, "my-app/dec.yaml", "name: my-app\nrequires: [shared-tools]\n")
	put(t, root, "my-app/public/user/skills/u/SKILL.md", "u")
	put(t, root, "my-app/public/project/rules/p.mdc", "p")
	put(t, root, "my-app/private/user/mcp/private.json", "{}")
	put(t, root, "my-app/private/project/commands/build/run.md", "build")

	got, err := Load(root, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Assets) != 4 {
		t.Fatalf("assets = %#v", got.Assets)
	}
	if got.Manifest.Requires[0] != "shared-tools" {
		t.Fatalf("requires = %#v", got.Manifest.Requires)
	}
	seenPrivateProject := false
	for _, asset := range got.Assets {
		if asset.Visibility == types.AssetVisibilityPrivate && types.CanonicalAssetPlane(asset.Plane) == types.AssetPlaneLocal {
			seenPrivateProject = true
		}
	}
	if !seenPrivateProject {
		t.Fatal("未枚举 private/project")
	}
}

func TestScanRejectsNonKebabPName(t *testing.T) {
	root := t.TempDir()
	put(t, root, "Bad_Name/dec.yaml", "name: Bad_Name\n")
	if _, err := Scan(root); err == nil {
		t.Fatal("非法 P 名应失败")
	}
}

func TestLoadRejectsSelfRequire(t *testing.T) {
	root := t.TempDir()
	put(t, root, "my-app/dec.yaml", "name: my-app\nrequires: [my-app]\n")
	if _, err := Load(root, "my-app"); err == nil {
		t.Fatal("自引用应失败")
	}
}

func TestScanIgnoresLegacyReservedDirectories(t *testing.T) {
	root := t.TempDir()
	put(t, root, "projects/dec.yaml", "name: Dec\n")
	put(t, root, "bundles/dec.yaml", "name: also-legacy\n")
	projects, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("reserved directories 不应识别为 P: %#v", projects)
	}
}

func TestScanRejectsUndeclaredTopLevelDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "orphan"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(root); err == nil {
		t.Fatal("顶层目录缺少 dec.yaml 应失败")
	}
}

func TestLoadRejectsAssetSymlink(t *testing.T) {
	root := t.TempDir()
	put(t, root, "my-app/dec.yaml", "name: my-app\n")
	outside := filepath.Join(root, "outside.mdc")
	put(t, root, "outside.mdc", "outside")
	link := filepath.Join(root, "my-app", "private", "project", "rules", "leak.mdc")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前平台无法创建符号链接: %v", err)
	}
	if _, err := Load(root, "my-app"); err == nil {
		t.Fatal("P 资产符号链接应失败")
	}
}
