package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

func TestEnsureVaultBundlesForUserEnable_CreatesMissing(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/cli/bundle.yaml": "name: cli\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	created, err := ensureVaultBundlesForUserEnable([]string{"cli", "woa"}, nil)
	if err != nil {
		t.Fatalf("ensureVaultBundlesForUserEnable() = %v", err)
	}
	if len(created) != 1 || created[0] != "woa" {
		t.Fatalf("created = %#v, 期望 [woa]", created)
	}

	tx, err := repo.NewReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	data, err := os.ReadFile(filepath.Join(tx.WorkDir(), "bundles", "woa", "bundle.yaml"))
	if err != nil {
		t.Fatalf("读取新建 bundle.yaml 失败: %v", err)
	}
	if !strings.Contains(string(data), "name: woa") {
		t.Fatalf("bundle.yaml = %s", data)
	}
	if !strings.Contains(string(data), "scope: user") {
		t.Fatalf("占位 bundle 应声明 scope: user, 实际: %s", data)
	}

	cliData, err := os.ReadFile(filepath.Join(tx.WorkDir(), "bundles", "cli", "bundle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cliData), "scope: user") {
		t.Fatalf("启用时已有 cli 应升级为 scope: user, 实际: %s", cliData)
	}
}

func TestEnsureVaultBundlesForUserEnable_UpgradesExistingProjectScope(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/cli/bundle.yaml": "name: cli\nscope: project\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	created, err := ensureVaultBundlesForUserEnable([]string{"cli"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 0 {
		t.Fatalf("created = %#v, 期望空（仅升级）", created)
	}
	tx, err := repo.NewReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	data, err := os.ReadFile(filepath.Join(tx.WorkDir(), "bundles", "cli", "bundle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "scope: user") {
		t.Fatalf("应升级为 scope: user, 实际: %s", data)
	}
}

// 平面隔离（ADR 0009）：project 上下文的 pull 不再并入用户平面启用列表。
func TestPullProjectAssets_IgnoresUserEnabledBundles(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/cli/skills/cli-skill/SKILL.md": "---\nname: cli-skill\n---\n",
		"bundles/cli/bundle.yaml": `name: cli
members:
  - skill/cli-skill
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote, EnabledBundles: []string{"cli"}}); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"cursor"},
		EnabledBundles: nil,
	}); err != nil {
		t.Fatal(err)
	}

	// 仅注入空 secrets client，避免 Bitwarden
	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() = %v", err)
	}
	if result.RequestedCount != 0 {
		t.Fatalf("RequestedCount = %d, 期望 0（用户平面启用不参与项目 pull）", result.RequestedCount)
	}
	if result.SkippedReason != "未启用 bundle" {
		t.Fatalf("SkippedReason = %q", result.SkippedReason)
	}
}
