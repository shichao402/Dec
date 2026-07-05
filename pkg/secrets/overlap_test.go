package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateNoOverlap_RejectsDecPrefix(t *testing.T) {
	projectRoot := t.TempDir()
	err := ValidateNoOverlap(projectRoot, []string{".dec/cache/vikunja/secret.toml"})
	if err == nil {
		t.Fatal("期望 .dec/ 前缀的 secrets 路径被拒绝")
	}
}

func TestValidateNoOverlap_RejectsIntersection(t *testing.T) {
	projectRoot := t.TempDir()
	conflict := filepath.Join(projectRoot, ".dec", "cache", "default", "skills", "foo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(conflict), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflict, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	err := ValidateNoOverlap(projectRoot, []string{".dec/cache/default/skills/foo/SKILL.md"})
	if err == nil {
		t.Fatal("期望与 .dec/ 树相交的路径被拒绝")
	}
}

func TestValidateNoOverlap_AllowsSeparateRoots(t *testing.T) {
	projectRoot := t.TempDir()
	skillPath := filepath.Join(projectRoot, ".dec", "cache", "default", "skills", "foo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("skill"), 0644); err != nil {
		t.Fatal(err)
	}

	err := ValidateNoOverlap(projectRoot, []string{".config/mise/conf.d/vikunja.toml"})
	if err != nil {
		t.Fatalf("期望独立根路径通过校验: %v", err)
	}
}

func TestPullBundle_WritesNotes(t *testing.T) {
	projectRoot := t.TempDir()
	client := &StubClient{
		NotesByFolder: map[string][]SecureNote{
			"vikunja": {{
				RelativePath: ".config/mise/conf.d/vikunja.toml",
				Content:      "[env]\nTOKEN=abc\n",
			}},
		},
	}
	paths, err := PullBundle(t.Context(), client, PullBundleRequest{
		ProjectRoot:   projectRoot,
		DecBundleName: "vikunja",
		Binding:       BundleBinding{DecBundleName: "vikunja", BitwardenFolder: "vikunja"},
	})
	if err != nil {
		t.Fatalf("PullBundle() 失败: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %#v, 期望 1 条", paths)
	}
	dest := filepath.Join(projectRoot, ".config", "mise", "conf.d", "vikunja.toml")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("落地文件应存在: %v", err)
	}
}
