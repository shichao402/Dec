package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
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
}

func TestMergeProjectAndUserEnabledBundles(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	if err := secrets.SaveConfig(&secrets.Config{UserEnabledBundles: []string{"woa", "vikunja"}}); err != nil {
		t.Fatal(err)
	}
	got, err := mergeProjectAndUserEnabledBundles([]string{"vikunja", "cli"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "vikunja" || got[1] != "cli" || got[2] != "woa" {
		t.Fatalf("merged = %#v", got)
	}
}

func TestPullProjectAssets_UsesUserEnabledWhenProjectEmpty(t *testing.T) {
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
	if err := secrets.SaveConfig(&secrets.Config{UserEnabledBundles: []string{"cli"}}); err != nil {
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
	if result.RequestedCount != 1 {
		t.Fatalf("RequestedCount = %d, 期望 1（用户级启用 cli）", result.RequestedCount)
	}
}
