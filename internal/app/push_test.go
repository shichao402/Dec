package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

func TestPushProjectAssets_SkipsDecWhenNothingEnabled(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{}); err != nil {
		t.Fatal(err)
	}

	var events []OperationEvent
	result, err := PushProjectAssets(context.Background(), projectRoot, ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("PushProjectAssets() = %v", err)
	}
	if result.DecSkippedReason == "" {
		t.Fatalf("DecSkippedReason 应为非空: %#v", result)
	}
	if !containsScopeMessage(events, "push.dec", "跳过 Dec 推送") {
		t.Fatalf("应发出 push.dec 跳过事件: %#v", events)
	}
	if !containsScopeMessage(events, "push.secrets", "推送") {
		t.Fatalf("默认 server_url 时应继续 push.secrets 阶段: %#v", events)
	}
}

func TestPushProjectAssets_PushesDecCacheChanges(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"combo/dec.yaml": "name: combo\n",
		"combo/public/project/skills/bundle-skill/SKILL.md": "---\nname: bundle-skill\n---\nold\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "combo",
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatal(err)
	}

	cacheSkill := filepath.Join(projectRoot, ".dec", "cache", "combo", "public", "project", "skills", "bundle-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(cacheSkill), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheSkill, []byte("---\nname: bundle-skill\n---\nnew content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var events []OperationEvent
	result, err := PushProjectAssets(context.Background(), projectRoot, ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("PushProjectAssets() = %v", err)
	}
	if result.DecPushedCount != 1 {
		t.Fatalf("DecPushedCount = %d, want 1", result.DecPushedCount)
	}
	if strings.TrimSpace(result.VersionCommit) == "" {
		t.Fatal("VersionCommit 应为非空")
	}
	if !containsScopeMessage(events, "push.dec", "Dec 推送完成") {
		t.Fatalf("应发出 push.dec 完成事件: %#v", events)
	}

	tx, err := repo.NewReadTransaction()
	if err != nil {
		t.Fatalf("NewReadTransaction() 失败: %v", err)
	}
	defer tx.Close()
	data, err := os.ReadFile(filepath.Join(tx.WorkDir(), "combo/public/project/skills/bundle-skill/SKILL.md"))
	if err != nil {
		t.Fatalf("读取远端资产失败: %v", err)
	}
	if !strings.Contains(string(data), "new content") {
		t.Fatalf("远端内容未更新: %q", string(data))
	}
}

func TestPushProjectAssets_SkipsDecWhenCacheMatchesRemote(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	content := "---\nname: bundle-skill\n---\nunchanged\n"
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"combo/dec.yaml": "name: combo\n",
		"combo/public/project/skills/bundle-skill/SKILL.md": content,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "combo",
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatal(err)
	}

	cacheSkill := filepath.Join(projectRoot, ".dec", "cache", "combo", "public", "project", "skills", "bundle-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(cacheSkill), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheSkill, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := PushProjectAssets(context.Background(), projectRoot, nil)
	if err != nil {
		t.Fatalf("PushProjectAssets() = %v", err)
	}
	if result.DecPushedCount != 0 {
		t.Fatalf("DecPushedCount = %d, want 0", result.DecPushedCount)
	}
	if result.DecSkippedReason != "无本地变更" {
		t.Fatalf("DecSkippedReason = %q, want 无本地变更", result.DecSkippedReason)
	}
}

func TestPushProjectAssets_RejectsLegacyRepositoryAndGuidesMigration(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/combo/bundle.yaml": "name: combo\nscope: project\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	projectRoot := t.TempDir()
	if err := config.NewProjectConfigManager(projectRoot).SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := PushProjectAssets(context.Background(), projectRoot, nil)
	if err == nil || !strings.Contains(err.Error(), "Push 已拒绝") || !strings.Contains(err.Error(), "P 迁移") {
		t.Fatalf("err = %v", err)
	}
}
