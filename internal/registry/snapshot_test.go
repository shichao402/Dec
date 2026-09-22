package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadProjectsListsOriginAndAssets(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("woa/provider.yaml", "origin_repo: https://github.com/shichao402/DecPersonalDevKit.git\ntags: [global]\n")
	write("woa/public/global/skills/gongfeng/SKILL.md", "# gongfeng\n")
	write("woa/public/global/mcp/gongfeng.json", "{}\n")
	write("yanked.yaml", "{}\n")

	projects, err := ReadProjects(root)
	if err != nil {
		t.Fatal(err)
	}
	woa, ok := projects["woa"]
	if !ok {
		t.Fatalf("projects = %#v", projects)
	}
	if woa.OriginRepo != "https://github.com/shichao402/DecPersonalDevKit.git" {
		t.Fatalf("origin = %q", woa.OriginRepo)
	}
	if len(woa.Tags) != 1 || woa.Tags[0] != "global" {
		t.Fatalf("tags = %#v", woa.Tags)
	}
	if len(woa.Assets) != 2 {
		t.Fatalf("assets = %#v", woa.Assets)
	}
	if woa.Assets[0].Type != "mcp" || woa.Assets[0].Name != "gongfeng" {
		t.Fatalf("first = %#v", woa.Assets[0])
	}
	if woa.Assets[1].Type != "skill" || woa.Assets[1].Name != "gongfeng" {
		t.Fatalf("second = %#v", woa.Assets[1])
	}
}
