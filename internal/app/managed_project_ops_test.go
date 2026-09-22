package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
)

func TestSuggestProjectNameMatchesConsole(t *testing.T) {
	cases := map[string]string{
		`D:\workspace\CppSvnAuthorAnalysis`: "cpp-svn-author-analysis",
		"/home/me/HTTPServerTools":          "http-server-tools",
		"/home/me/my_project.v2/":           "my-project-v2",
		"/home/me/__agents help me__":       "agents-help-me",
		"/home/me/dec":                      "dec",
	}
	for root, want := range cases {
		if got := SuggestProjectName(root); got != want {
			t.Fatalf("SuggestProjectName(%q) = %q, want %q", root, got, want)
		}
	}
}

func TestPurgeManagedProjectCleansOnlyTarget(t *testing.T) {
	home := t.TempDir()
	decHome := filepath.Join(home, ".dec")
	setEnvForProjectTest(t, "HOME", home)
	setEnvForProjectTest(t, "DEC_HOME", decHome)

	target := filepath.Join(home, "CppSvnAuthorAnalysis")
	neighbor := filepath.Join(home, "DocAgent")
	for _, root := range []string{target, neighbor} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := config.RegisterManagedProject(root, filepath.Base(root)); err != nil {
			t.Fatal(err)
		}
		mustWriteCleanupFile(t, filepath.Join(root, ".dec", "config.yaml"), "project_name: demo\n")
		mustWriteCleanupFile(t, filepath.Join(root, ".dec", "cache", "x"), "cache")
		mustWriteCleanupFile(t, filepath.Join(root, "README.md"), "keep me\n")
		mustWriteCleanupFile(t, filepath.Join(root, ".secrets", "p", ".env", "x.env"), "X=1\n")
		mustWriteCleanupFile(t, filepath.Join(root, filepath.FromSlash(".secrets/dec/integration/bitwarden.yaml")), "email: keep\n")
	}
	mustWriteCleanupFile(t, filepath.Join(target, ".cursor", "skills", "dec-owned", "SKILL.md"), "# owned\n")
	mcpPath := filepath.Join(target, ".cursor", "mcp.json")
	mcp := map[string]any{
		"mcpServers": map[string]any{
			"dec":  map[string]any{"command": "dec-mcp"},
			"user": map[string]any{"command": "user-mcp"},
		},
	}
	data, _ := json.Marshal(mcp)
	mustWriteCleanupFile(t, mcpPath, string(data))

	result, err := PurgeManagedProject(context.Background(), target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Removed {
		t.Fatalf("应摘掉受管登记: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(target, ".dec")); !os.IsNotExist(err) {
		t.Fatalf("目标 .dec 应删除: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".secrets", "p")); !os.IsNotExist(err) {
		t.Fatalf("目标 Dec secrets 落地应删除: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Fatalf("业务源码应保留: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(".secrets/dec/integration/bitwarden.yaml"))); err != nil {
		t.Fatalf("integration 凭据应保留: %v", err)
	}
	if _, err := os.Stat(filepath.Join(neighbor, ".dec", "config.yaml")); err != nil {
		t.Fatalf("邻项 .dec 应保留: %v", err)
	}
	items, err := config.ListManagedProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Root != neighbor {
		t.Fatalf("受管列表应只剩邻项: %#v", items)
	}
}

func TestAutoInitManagedProjectBindsWhenNameMatches(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"cpp-svn-author-analysis/dec.yaml": "name: cpp-svn-author-analysis\n",
		"cpp-svn-author-analysis/public/project/skills/demo/SKILL.md": "---\nname: demo\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "CppSvnAuthorAnalysis")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := config.RegisterManagedProject(root, ""); err != nil {
		t.Fatal(err)
	}

	result, err := AutoInitManagedProject(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped || !result.Initialized || result.HomeProject != "cpp-svn-author-analysis" {
		t.Fatalf("应绑定同名家项目: %#v", result)
	}
	cfg, err := config.NewProjectConfigManager(root).LoadProjectConfig()
	if err != nil || cfg == nil || cfg.ProjectName != "cpp-svn-author-analysis" {
		t.Fatalf("配置未写入: cfg=%#v err=%v", cfg, err)
	}
}

func TestAutoInitManagedProjectSkipsWithoutMatch(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"other-app/dec.yaml": "name: other-app\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "CppSvnAuthorAnalysis")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := AutoInitManagedProject(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped || result.Initialized || result.Reason != "no matching home project" {
		t.Fatalf("无匹配应跳过: %#v", result)
	}
	if config.NewProjectConfigManager(root).Exists() {
		t.Fatal("跳过时不应写 .dec")
	}
}
