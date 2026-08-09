package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
)

func TestAddProjectSecret_CreatesNoteNamedBySyncRelPath(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	stub := &secrets.StubClient{}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{EnabledBundles: []string{"tencent-cloud"}}); err != nil {
		t.Fatal(err)
	}
	writeProjectFileForPushTest(t, projectRoot, ".secrets/bundles/tencent-cloud/env/tencent.env", "SECRET_ID=abc\n")

	result, err := AddProjectSecret(context.Background(), projectRoot, "tencent-cloud", "env/tencent.env", nil)
	if err != nil {
		t.Fatalf("AddProjectSecret() = %v", err)
	}
	if result.Folder != "tencent-cloud" || result.LandingPath != ".secrets/bundles/tencent-cloud/env/tencent.env" {
		t.Fatalf("result = %#v", result)
	}
	notes := stub.NotesByFolder["tencent-cloud"]
	if len(notes) != 1 || notes[0].RelativePath != "env/tencent.env" {
		t.Fatalf("notes = %#v, note 名应相对同步根", notes)
	}
	if notes[0].Content != "SECRET_ID=abc\n" {
		t.Fatalf("正文 = %q", notes[0].Content)
	}
}

// 先有文件、再登记：反过来会在 Bitwarden 里留一条指向不存在路径的 Note。
func TestAddProjectSecret_RejectsMissingFile(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return &secrets.StubClient{} }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{EnabledBundles: []string{"tencent-cloud"}}); err != nil {
		t.Fatal(err)
	}

	_, err := AddProjectSecret(context.Background(), projectRoot, "tencent-cloud", "env/absent.env", nil)
	if err == nil {
		t.Fatal("文件不存在时应报错")
	}
	if !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("错误应说明文件不存在: %v", err)
	}
}

func TestAddProjectSecret_RejectsDirectory(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return &secrets.StubClient{} }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{EnabledBundles: []string{"tencent-cloud"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, ".secrets", "bundles", "tencent-cloud", "config"), 0755); err != nil {
		t.Fatal(err)
	}

	_, err := AddProjectSecret(context.Background(), projectRoot, "tencent-cloud", "config", nil)
	if err == nil || !strings.Contains(err.Error(), "目录") {
		t.Fatalf("目录应被拒绝: %v", err)
	}
}

func TestAddProjectSecret_RejectsPathEscapingProjectRoot(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return &secrets.StubClient{} }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	if _, err := AddProjectSecret(context.Background(), t.TempDir(), "evil", "../outside.yaml", nil); err == nil {
		t.Fatal("逃逸项目根的路径应被拒绝")
	}
}

func TestSuggestSecretFolders_ProjectFolderFirst(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "Demo",
		EnabledBundles: []string{"vikunja", "combo"},
	}); err != nil {
		t.Fatal(err)
	}

	folders, err := SuggestSecretFolders(projectRoot)
	if err != nil {
		t.Fatalf("SuggestSecretFolders() = %v", err)
	}
	if len(folders) == 0 || folders[0] != "Demo" {
		t.Fatalf("folders = %#v, 期望 project folder 打头", folders)
	}
	if len(folders) != 3 {
		t.Fatalf("folders = %#v, 期望 project + 2 个 bundle folder", folders)
	}
}
