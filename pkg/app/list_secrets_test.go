package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
	"gopkg.in/yaml.v3"
)

// 落地路径就是消费者路径，散在项目根，扫目录无法区分哪些文件归 Bitwarden 管。
// 不查远端时只能给空列表 + 明确理由，不能靠猜。
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
			"vikunja_workflow": {{RelativePath: ".config/mise/conf.d/vikunja.toml", Content: "SECRET=1"}},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = orig })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}
	landed := filepath.Join(projectRoot, ".config", "mise", "conf.d", "vikunja.toml")
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
	if file.SecretsBundle != "vikunja_workflow" || file.ProjectRelPath != ".config/mise/conf.d/vikunja.toml" {
		t.Fatalf("元数据 = %#v", file)
	}
	if file.RemoteExists == nil || !*file.RemoteExists {
		t.Fatalf("RemoteExists = %#v", file.RemoteExists)
	}
	if !file.LocalExists || file.LocalSizeBytes == 0 {
		t.Fatalf("本地元数据 = %#v", file)
	}
}

func TestMCPSessionUnlockTimeout(t *testing.T) {
	if MCPSessionUnlockTimeout != 3*time.Minute {
		t.Fatalf("MCPSessionUnlockTimeout = %v", MCPSessionUnlockTimeout)
	}
}
