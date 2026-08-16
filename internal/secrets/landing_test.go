package secrets

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeSyncRelPath_RejectsUntrustedNames(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"",
		"   ",
		"/etc/passwd",
		"~/.ssh/id_rsa",
		"~",
		"../outside.env",
		"a/../../outside.env",
		"..",
		`..\..\outside.env`,
		"C:/Windows/system32",
	} {
		if got, err := normalizeSyncRelPath(raw); err == nil {
			t.Fatalf("normalizeSyncRelPath(%q) = %q, 期望报错", raw, got)
		}
	}
}

func TestNormalizeSyncRelPath_KeepsBarePathWithoutDotSlash(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"config/private.yaml":   "config/private.yaml",
		"env/app.env":           "env/app.env",
		"./config/private.yaml": "config/private.yaml",
		"env//nested/./app.env": "env/nested/app.env",
	} {
		got, err := normalizeSyncRelPath(raw)
		if err != nil {
			t.Fatalf("normalizeSyncRelPath(%q) 报错: %v", raw, err)
		}
		if got != want {
			t.Fatalf("normalizeSyncRelPath(%q) = %q, 期望 %q", raw, got, want)
		}
	}
}

func TestValidateLandingPaths_RejectsCrossFolderCollision(t *testing.T) {
	t.Parallel()

	err := ValidateLandingPaths(t.TempDir(), []LandingCandidate{
		{Folder: "tencent-cloud", LocalRoot: ".secrets/bundles/shared", RelativePath: "env/app.env"},
		{Folder: "my-project", LocalRoot: ".secrets/bundles/shared", RelativePath: "env/app.env"},
	})
	if err == nil {
		t.Fatal("两个 folder 撞同一落地路径时应报错")
	}
	for _, want := range []string{"env/app.env", "tencent-cloud", "my-project"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("冲突错误应点明 %q:\n%v", want, err)
		}
	}
}

func TestValidateLandingPaths_AllowsSameFolderDuplicate(t *testing.T) {
	t.Parallel()

	err := ValidateLandingPaths(t.TempDir(), []LandingCandidate{
		{Folder: "vikunja", LocalRoot: ".secrets/bundles/vikunja", RelativePath: "env/vikunja.env"},
		{Folder: "vikunja", LocalRoot: ".secrets/bundles/vikunja", RelativePath: "env/vikunja.env"},
	})
	if err != nil {
		t.Fatalf("同 folder 内重复路径应视为去重而非冲突: %v", err)
	}
}

func TestValidateLandingPaths_RejectsDecOverlap(t *testing.T) {
	t.Parallel()

	err := ValidateLandingPaths(t.TempDir(), []LandingCandidate{
		{Folder: "evil", LocalRoot: ".secrets/bundles/evil", RelativePath: ".dec/config.yaml"},
	})
	if err == nil {
		t.Fatal("落地路径指向 .dec/ 时应报错")
	}
}

func TestValidateLandingPaths_RejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	outside := t.TempDir()
	secretsRoot := filepath.Join(projectRoot, ".secrets", "bundles", "evil")
	if err := os.MkdirAll(secretsRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(secretsRoot, "escape")); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	err := ValidateLandingPaths(projectRoot, []LandingCandidate{
		{Folder: "evil", LocalRoot: ".secrets/bundles/evil", RelativePath: "escape/stolen.env"},
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

	tracked := filepath.Join(projectRoot, ".secrets", "project", "config", "private.yaml")
	if err := os.MkdirAll(filepath.Dir(tracked), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracked, []byte("placeholder\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitOrSkip(t, projectRoot, "add", ".secrets/project/config/private.yaml")

	err := ValidateLandingPaths(projectRoot, []LandingCandidate{
		{Folder: "my-project", LocalRoot: ".secrets/project", RelativePath: "config/private.yaml"},
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
		{Folder: "my-project", LocalRoot: ".secrets/project", RelativePath: "config/private.yaml"},
	}); err != nil {
		t.Fatalf("未被跟踪的路径应通过校验: %v", err)
	}
}

func TestUnignoredLandingPaths(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)
	if err := os.WriteFile(filepath.Join(projectRoot, ".gitignore"), []byte("/.secrets/project/env/\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := UnignoredLandingPaths(projectRoot, []string{".secrets/project/env/app.env", ".secrets/project/config/private.yaml"})
	if len(got) != 1 || got[0] != ".secrets/project/config/private.yaml" {
		t.Fatalf("UnignoredLandingPaths() = %#v, 期望仅 .secrets/project/config/private.yaml", got)
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

func skipUnlessUnixFileMode(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上文件权限语义与 Unix 不同")
	}
}
