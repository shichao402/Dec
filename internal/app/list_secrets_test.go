package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/registry"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

// 密文落在 .secrets/ 同步根，不查远端时只能给空列表 + 明确理由。
func TestListSecretsMetadata_WithoutRemoteReturnsNothingAndSaysWhy(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, ".env.local"), []byte("TOKEN=x\n"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := ListSecretsMetadata(context.Background(), projectRoot, false, nil)
	if err != nil {
		t.Fatalf("ListSecretsMetadata() = %v", err)
	}
	if len(result.Files) != 0 {
		t.Fatalf("files = %#v, 期望空", result.Files)
	}
	if !strings.Contains(result.SkippedReason, "includeRemote") {
		t.Fatalf("SkippedReason = %q, 应说明需要查远端", result.SkippedReason)
	}
}

func TestListSecretsMetadata_IncludeRemoteUsesStubWithoutContent(t *testing.T) {
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

	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"vikunja/private/project": {{RelativePath: ".env/vikunja.env", Content: "VIKUNJA_API_TOKEN=abc\n"}},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = orig })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		Requires: types.RequiresSpec{"vikunja": types.RequiresVault},
	}); err != nil {
		t.Fatal(err)
	}
	landed := filepath.Join(projectRoot, ".secrets", "vikunja", ".env", "vikunja.env")
	if err := os.MkdirAll(filepath.Dir(landed), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(landed, []byte("SECRET=1"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := ListSecretsMetadata(context.Background(), projectRoot, true, nil)
	if err != nil {
		t.Fatalf("ListSecretsMetadata() = %v", err)
	}
	if !result.RemoteChecked {
		t.Fatalf("expected remote checked, got %#v", result)
	}
	if len(result.Files) != 1 {
		t.Fatalf("files = %#v, 期望 1 条", result.Files)
	}
	file := result.Files[0]
	if file.SecretsBundle != "vikunja/private/local" || file.ProjectRelPath != ".secrets/vikunja/.env/vikunja.env" {
		t.Fatalf("元数据 = %#v", file)
	}
	// TestMain 把 registry 快照置为不可达：归属字段留空，绝不误判孤儿（ADR 0034 容错红线）。
	if file.Orphan || file.IdentityOnly || file.OriginRepo != "" {
		t.Fatalf("registry 不可达时归属字段应留空: %#v", file)
	}
	if file.RemoteExists == nil || !*file.RemoteExists {
		t.Fatalf("RemoteExists = %#v", file.RemoteExists)
	}
	if !file.LocalExists || file.LocalSizeBytes == 0 {
		t.Fatalf("本地元数据 = %#v", file)
	}
}

// ADR 0034：归属 join 三态。正常产品带 origin_repo；身份型标 identity_only；
// registry 查无的 folder 标 orphan。registry 不可达时全部留空（见上一用例）。
func TestListSecretsMetadata_BelongingJoin(t *testing.T) {
	const kitRepo = "https://example.com/DecPersonalDevKit.git"
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	stubBelongingSnapshots(t, map[string]registry.ProjectSnapshot{
		"cnb":           {OriginRepo: kitRepo, Assets: []registry.SnapshotAsset{{Name: "demo"}}},
		"tencent-cloud": {OriginRepo: kitRepo},
	})
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

	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"cnb/private/project":           {{RelativePath: ".env/cnb.env", Content: "T=1\n"}},
			"tencent-cloud/private/project": {{RelativePath: ".env/tc.env", Content: "T=1\n"}},
			"nobody/private/project":        {{RelativePath: ".env/nobody.env", Content: "T=1\n"}},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = orig })

	result, err := ListSecretsMetadata(context.Background(), t.TempDir(), true, nil)
	if err != nil {
		t.Fatalf("ListSecretsMetadata() = %v", err)
	}
	byFolder := make(map[string]SecretFileMetadata)
	for _, file := range result.Files {
		byFolder[file.SecretsBundle] = file
	}
	if got := len(byFolder); got != 3 {
		t.Fatalf("files = %#v, 期望 3 个 folder", result.Files)
	}
	if file := byFolder["cnb/private/local"]; file.OriginRepo != kitRepo || file.IdentityOnly || file.Orphan {
		t.Fatalf("正常产品 = %#v", file)
	}
	if file := byFolder["tencent-cloud/private/local"]; !file.IdentityOnly || file.Orphan || file.OriginRepo != kitRepo {
		t.Fatalf("身份型产品 = %#v", file)
	}
	if file := byFolder["nobody/private/local"]; !file.Orphan || file.IdentityOnly || file.OriginRepo != "" {
		t.Fatalf("疑似孤儿 = %#v", file)
	}
}

// 用户平面的 secrets 落在 ~/.dec/secrets 下，Root 必须为空（ADR 0015）；
// 拿空 Root 当「缺项目根」报错会让 Console 的本机平面永远列不出 secrets。
func TestListWorkspaceSecretsMetadata_UserPlaneAcceptsEmptyRoot(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)

	result, err := ListWorkspaceSecretsMetadata(
		context.Background(), NewWorkspace(WorkspaceUser, ""), false, nil)
	if err != nil {
		t.Fatalf("ListWorkspaceSecretsMetadata(user, \"\") = %v", err)
	}
	if !strings.Contains(result.SkippedReason, "includeRemote") {
		t.Fatalf("SkippedReason = %q, 应说明需要查远端", result.SkippedReason)
	}
}

// 项目平面反过来必须拒绝空 Root：否则 .secrets/ 会落到服务 cwd 下。
func TestListWorkspaceSecretsMetadata_ProjectPlaneRejectsEmptyRoot(t *testing.T) {
	_, err := ListWorkspaceSecretsMetadata(
		context.Background(), NewWorkspace(WorkspaceProject, ""), false, nil)
	if err == nil {
		t.Fatal("ListWorkspaceSecretsMetadata(project, \"\") 应报错")
	}
}

func TestMCPSessionUnlockTimeout(t *testing.T) {
	if MCPSessionUnlockTimeout != 3*time.Minute {
		t.Fatalf("MCPSessionUnlockTimeout = %v", MCPSessionUnlockTimeout)
	}
}
