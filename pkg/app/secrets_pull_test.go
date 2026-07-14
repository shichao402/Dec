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
	"gopkg.in/yaml.v3"
)

func TestPullProjectAssets_UsesDefaultServerWithoutConfigFile(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/skills/project-workflow/SKILL.md": "---\nname: project-workflow\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs: []string{"cursor"},
		Available: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
		EnabledBundles: []string{"default"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	var events []OperationEvent
	result, err := PullProjectAssets(context.Background(), projectRoot, "", ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.PulledCount != 1 {
		t.Fatalf("PulledCount = %d, 期望 1", result.PulledCount)
	}
	if containsScopeMessage(events, "pull.secrets", "Bitwarden 未配置") {
		t.Fatalf("默认 server_url 时不应跳过 secrets: %#v", events)
	}
}

func TestPullProjectAssets_RejectsSecretsOverlap(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := secrets.Config{ServerURL: "https://vault.example.com"}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"combo": {{
					RelativePath: ".dec/cache/combo/skills/bundle-skill/SKILL.md",
					Content:      "secret",
				}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/combo/skills/bundle-skill/SKILL.md": "---\nname: bundle-skill\n---\n",
		"bundles/combo/bundle.yaml": `name: combo
members:
  - skill/bundle-skill
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	_, err = PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err == nil {
		t.Fatal("期望 secrets 路径重叠时 pull 失败")
	}
	if !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("错误应描述路径冲突: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-bundle-skill", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("重叠校验失败时不应安装 IDE 资产")
	}
}

func containsScopeMessage(events []OperationEvent, scope, fragment string) bool {
	for _, event := range events {
		if event.Scope == scope && strings.Contains(event.Message, fragment) {
			return true
		}
	}
	return false
}

func TestCleanupRemovedSecretsBundlesKeepsProjectSecretsCaseInsensitive(t *testing.T) {
	projectRoot := t.TempDir()
	vikunjaDir := filepath.Join(projectRoot, ".secrets", "vikunja_workflow", "mise")
	if err := os.MkdirAll(vikunjaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vikunjaDir, "x.toml"), []byte("a=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(projectRoot, ".secrets", "dec", "integration")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "keep.yaml"), []byte("ok: true\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cleaned := cleanupRemovedSecretsBundles(projectRoot, &secretsSyncPlan{
		ProjectSecretsName: "Dec",
	}, &secrets.Config{})
	if len(cleaned) != 1 || cleaned[0] != "vikunja_workflow" {
		t.Fatalf("cleaned = %#v, 期望 [vikunja_workflow]", cleaned)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "vikunja_workflow")); !os.IsNotExist(err) {
		t.Fatalf("vikunja_workflow 应被删除, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "dec", "integration", "keep.yaml")); err != nil {
		t.Fatalf("project secrets 目录应保留: %v", err)
	}
}

func TestPruneLocalSecretsBundlesCleansStaleWithoutBitwarden(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "Demo"}); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(projectRoot, ".secrets", "vikunja_workflow", "mise")
	if err := os.MkdirAll(stale, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "x.toml"), []byte("a=1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	var events []OperationEvent
	cleaned, err := pruneLocalSecretsBundles(projectRoot, nil, ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("pruneLocalSecretsBundles() 失败: %v", err)
	}
	if len(cleaned) != 1 || cleaned[0] != "vikunja_workflow" {
		t.Fatalf("cleaned = %#v", cleaned)
	}
	if !containsScopeMessage(events, "pull.secrets.cleanup", "vikunja_workflow") {
		t.Fatalf("应发出 secrets cleanup 事件: %#v", events)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "vikunja_workflow")); !os.IsNotExist(err) {
		t.Fatalf("过期 secrets bundle 应删除, err=%v", err)
	}
}
