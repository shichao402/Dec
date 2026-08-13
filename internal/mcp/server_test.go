package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/service"
	"github.com/shichao402/Dec/internal/serviceapi"
	"github.com/shichao402/Dec/internal/servicehost"
)

func TestHandleStatus_NoConfig(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	startTestService(t)
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
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T", resp.Data)
	}
	overview, ok := data["project"].(*app.ProjectOverview)
	if !ok {
		t.Fatalf("project data type = %T", data["project"])
	}
	if overview.ProjectRoot != root {
		t.Fatalf("ProjectRoot = %q", overview.ProjectRoot)
	}
}

func TestHandleConnectRepoAndInit(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	startTestService(t)

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

func startTestService(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- servicehost.Run(ctx, "test") }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := service.ReadMetadata(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("dec-server 测试实例未就绪")
		}
		time.Sleep(20 * time.Millisecond)
	}
	api, err := serviceapi.Connect(context.Background(), "mcp-test", "mcp-test")
	if err != nil {
		cancel()
		t.Fatalf("连接测试 dec-server: %v", err)
	}
	serviceapi.SetDefault(api)
	t.Cleanup(func() {
		_ = api.Close()
		serviceapi.SetDefault(nil)
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("测试 dec-server 未退出")
		}
	})
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
