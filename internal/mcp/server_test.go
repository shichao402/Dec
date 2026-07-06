package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/pkg/app"
	"github.com/shichao402/Dec/pkg/repo"
)

func TestHandleStatus_NoConfig(t *testing.T) {
	root := t.TempDir()
	s := New(Config{ProjectRoot: root})

	_, out, err := s.handleStatus(context.Background(), nil, statusParams{})
	if err != nil {
		t.Fatalf("handleStatus() err = %v", err)
	}
	resp, ok := out.(toolResponse)
	if !ok {
		t.Fatalf("output type = %T", out)
	}
	if !resp.OK {
		t.Fatalf("expected ok, got %#v", resp)
	}
	overview, ok := resp.Data.(*app.ProjectOverview)
	if !ok {
		t.Fatalf("data type = %T", resp.Data)
	}
	if overview.ProjectRoot != root {
		t.Fatalf("ProjectRoot = %q", overview.ProjectRoot)
	}
}

func TestHandleConnectRepoAndInit(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	remote := setupRemoteRepo(t, map[string]string{
		"bundles/default/skills/helloworld/SKILL.md": "---\nname: helloworld\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect: %v", err)
	}

	projectRoot := t.TempDir()
	s := New(Config{ProjectRoot: projectRoot})

	_, connectOut, err := s.handleConnectRepo(context.Background(), nil, connectRepoParams{RepoURL: remote})
	connectResp, _ := connectOut.(toolResponse)
	if err != nil || !connectResp.OK {
		t.Fatalf("handleConnectRepo() = %#v, %v", connectResp, err)
	}

	_, initOut, err := s.handleInitProject(context.Background(), nil, initProjectParams{})
	initResp, _ := initOut.(toolResponse)
	if err != nil || !initResp.OK {
		t.Fatalf("handleInitProject() = %#v, %v", initResp, err)
	}
}

func TestResolveProjectRoot(t *testing.T) {
	t.Setenv("DEC_PROJECT_ROOT", "")
	if got := resolveProjectRoot("/explicit"); got != "/explicit" {
		t.Fatalf("resolveProjectRoot explicit = %q", got)
	}
	t.Setenv("DEC_PROJECT_ROOT", "/from-env")
	if got := resolveProjectRoot(""); got != "/from-env" {
		t.Fatalf("resolveProjectRoot env = %q", got)
	}
}

func setupRemoteRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	work := t.TempDir()
	bare := t.TempDir()

	runGit(t, work, "init")
	runGit(t, work, "config", "user.email", "test@test.com")
	runGit(t, work, "config", "user.name", "test")
	for path, content := range files {
		full := filepath.Join(work, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "init")
	runGit(t, bare, "init", "--bare")
	runGit(t, work, "remote", "add", "origin", bare)
	runGit(t, work, "push", "-u", "origin", "HEAD")
	return bare
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}
