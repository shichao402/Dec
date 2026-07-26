package secrets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeProjectRelativePath_RejectsUntrustedNames(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"",
		"   ",
		"/etc/passwd",
		"~/.ssh/id_rsa",
		"~",
		"../outside.toml",
		"a/../../outside.toml",
		"..",
		`..\..\outside.toml`,
		"C:/Windows/system32",
	} {
		if got, err := normalizeProjectRelativePath(raw); err == nil {
			t.Fatalf("normalizeProjectRelativePath(%q) = %q, 期望报错", raw, got)
		}
	}
}

func TestNormalizeProjectRelativePath_KeepsBarePathWithoutDotSlash(t *testing.T) {
	t.Parallel()

	// 落地路径要和 Bitwarden Note 名逐字一致，补 "./" 会让两边对不上。
	for raw, want := range map[string]string{
		"config/private.yaml":           "config/private.yaml",
		".env.local":                    ".env.local",
		"./config/private.yaml":         "config/private.yaml",
		".config/mise/conf.d/x.toml":    ".config/mise/conf.d/x.toml",
		"config//nested/./private.yaml": "config/nested/private.yaml",
	} {
		got, err := normalizeProjectRelativePath(raw)
		if err != nil {
			t.Fatalf("normalizeProjectRelativePath(%q) 报错: %v", raw, err)
		}
		if got != want {
			t.Fatalf("normalizeProjectRelativePath(%q) = %q, 期望 %q", raw, got, want)
		}
	}
}

func TestValidateLandingPaths_RejectsCrossFolderCollision(t *testing.T) {
	t.Parallel()

	err := ValidateLandingPaths(t.TempDir(), []LandingCandidate{
		{Folder: "tencent-cloud", RelativePath: ".env.local"},
		{Folder: "my-project", RelativePath: ".env.local"},
	})
	if err == nil {
		t.Fatal("两个 folder 撞同一落地路径时应报错")
	}
	for _, want := range []string{".env.local", "tencent-cloud", "my-project"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("冲突错误应点明 %q:\n%v", want, err)
		}
	}
}

func TestValidateLandingPaths_AllowsSameFolderDuplicate(t *testing.T) {
	t.Parallel()

	err := ValidateLandingPaths(t.TempDir(), []LandingCandidate{
		{Folder: "vikunja_workflow", RelativePath: ".config/mise/conf.d/vikunja.toml"},
		{Folder: "vikunja_workflow", RelativePath: ".config/mise/conf.d/vikunja.toml"},
	})
	if err != nil {
		t.Fatalf("同 folder 内重复路径应视为去重而非冲突: %v", err)
	}
}

func TestValidateLandingPaths_RejectsDecOverlap(t *testing.T) {
	t.Parallel()

	err := ValidateLandingPaths(t.TempDir(), []LandingCandidate{
		{Folder: "evil", RelativePath: ".dec/config.yaml"},
	})
	if err == nil {
		t.Fatal("落地路径指向 .dec/ 时应报错")
	}
}

func TestValidateLandingPaths_RejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(projectRoot, "escape")); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	err := ValidateLandingPaths(projectRoot, []LandingCandidate{
		{Folder: "evil", RelativePath: "escape/stolen.toml"},
	})
	if err == nil {
		t.Fatal("落地路径经符号链接指向项目外时应报错")
	}
	if !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("错误应说明是符号链接逃逸:\n%v", err)
	}
}

func TestValidateLandingPaths_RejectsGitTrackedPath(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	tracked := filepath.Join(projectRoot, "config", "private.yaml")
	if err := os.MkdirAll(filepath.Dir(tracked), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracked, []byte("placeholder\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitOrSkip(t, projectRoot, "add", "config/private.yaml")

	err := ValidateLandingPaths(projectRoot, []LandingCandidate{
		{Folder: "my-project", RelativePath: "config/private.yaml"},
	})
	if err == nil {
		t.Fatal("落地路径已被 git 跟踪时应硬失败")
	}
	if !strings.Contains(err.Error(), "git rm --cached") {
		t.Fatalf("错误应给出处置方式:\n%v", err)
	}
}

func TestValidateLandingPaths_AllowsUntrackedPathInGitRepo(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	if err := ValidateLandingPaths(projectRoot, []LandingCandidate{
		{Folder: "my-project", RelativePath: "config/private.yaml"},
	}); err != nil {
		t.Fatalf("未被跟踪的路径应通过校验: %v", err)
	}
}

func TestUnignoredLandingPaths(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)
	if err := os.WriteFile(filepath.Join(projectRoot, ".gitignore"), []byte("/.env.local\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := UnignoredLandingPaths(projectRoot, []string{".env.local", "config/private.yaml"})
	if len(got) != 1 || got[0] != "config/private.yaml" {
		t.Fatalf("UnignoredLandingPaths() = %#v, 期望仅 config/private.yaml", got)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGitOrSkip(t, dir, "init", "--quiet")
	runGitOrSkip(t, dir, "config", "user.email", "test@example.com")
	runGitOrSkip(t, dir, "config", "user.name", "test")
}

func runGitOrSkip(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git %v 失败（跳过依赖 git 的用例）: %v\n%s", args, err, out)
	}
}
