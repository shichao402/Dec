package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

func tencentProjectScope() secrets.RemoteScope {
	return secrets.RemoteScope{P: "tencent-cloud", Plane: secrets.SyncPlaneProject}
}

// enableBundlesForAddTest 让 tencent-cloud 成为当前平面已启用的项目。
func enableBundlesForAddTest(t *testing.T, projectRoot string, names ...string) {
	t.Helper()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{EnabledBundles: names}); err != nil {
		t.Fatal(err)
	}
}

func TestAddProjectSecretForScope_CreatesNoteNamedBySyncRelPath(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	stub := &secrets.StubClient{}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	enableBundlesForAddTest(t, projectRoot, "tencent-cloud")
	scope := tencentProjectScope()
	target, err := secrets.NewPSyncTarget(scope.P, scope.Plane)
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFileForPushTest(t, projectRoot, target.LocalRoot+"/.env/tencent.env", "SECRET_ID=abc\n")

	result, err := AddProjectSecretForScope(context.Background(), projectRoot, scope, ".env/tencent.env", nil)
	if err != nil {
		t.Fatalf("AddProjectSecretForScope() = %v", err)
	}
	if result.Address != scope.String() || result.LandingPath != target.LocalRoot+"/.env/tencent.env" {
		t.Fatalf("result = %#v", result)
	}
	notes := stub.NotesByFolder[scope.String()]
	if len(notes) != 1 || notes[0].RelativePath != ".env/tencent.env" {
		t.Fatalf("notes = %#v, note 名应相对同步根", notes)
	}
	if notes[0].Content != "SECRET_ID=abc\n" {
		t.Fatalf("正文 = %q", notes[0].Content)
	}
}

// 相对路径带上同步根前缀时也应被剥掉，而不是登记成 .secrets/... 的 note 名。
func TestAddProjectSecretForScope_StripsSyncRootPrefix(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	stub := &secrets.StubClient{}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	enableBundlesForAddTest(t, projectRoot, "tencent-cloud")
	scope := tencentProjectScope()
	target, err := secrets.NewPSyncTarget(scope.P, scope.Plane)
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFileForPushTest(t, projectRoot, target.LocalRoot+"/.env/tencent.env", "SECRET_ID=abc\n")

	result, err := AddProjectSecretForScope(context.Background(), projectRoot, scope,
		target.LocalRoot+"/.env/tencent.env", nil)
	if err != nil {
		t.Fatalf("AddProjectSecretForScope() = %v", err)
	}
	if result.NoteRelPath != ".env/tencent.env" {
		t.Fatalf("note 名应剥掉同步根前缀: %#v", result)
	}
}

// 先有文件、再登记：反过来会在 Bitwarden 里留一条指向不存在路径的 Note。
func TestAddProjectSecretForScope_RejectsMissingFile(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return &secrets.StubClient{} }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	enableBundlesForAddTest(t, projectRoot, "tencent-cloud")

	_, err := AddProjectSecretForScope(context.Background(), projectRoot, tencentProjectScope(), ".env/absent.env", nil)
	if err == nil {
		t.Fatal("文件不存在时应报错")
	}
	if !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("错误应说明文件不存在: %v", err)
	}
}

func TestAddProjectSecretForScope_RejectsDirectory(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return &secrets.StubClient{} }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	enableBundlesForAddTest(t, projectRoot, "tencent-cloud")
	scope := tencentProjectScope()
	target, err := secrets.NewPSyncTarget(scope.P, scope.Plane)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, filepath.FromSlash(target.LocalRoot), "config"), 0755); err != nil {
		t.Fatal(err)
	}

	_, err = AddProjectSecretForScope(context.Background(), projectRoot, scope, "config", nil)
	if err == nil || !strings.Contains(err.Error(), "目录") {
		t.Fatalf("目录应被拒绝: %v", err)
	}
}

func TestAddProjectSecretForScope_RejectsPathEscapingProjectRoot(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return &secrets.StubClient{} }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	if _, err := AddProjectSecretForScope(context.Background(), t.TempDir(),
		secrets.RemoteScope{P: "evil", Plane: secrets.SyncPlaneProject}, "../outside.yaml", nil); err == nil {
		t.Fatal("逃逸项目根的路径应被拒绝")
	}
}

// 非法项目名不能通过 scope 进入写入路径。
func TestAddProjectSecretForScope_RejectsInvalidPName(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	if _, err := AddProjectSecretForScope(context.Background(), t.TempDir(),
		secrets.RemoteScope{P: "Bad Name", Plane: secrets.SyncPlaneProject}, ".env/x.env", nil); err == nil {
		t.Fatal("非法项目名应被拒绝")
	}
}

// 项目平面只有本项目的 private/project 可写：启用的其它项目只带 Git 资产。
func TestSuggestSecretAddresses_OnlyHomeProjectOnProjectPlane(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "demo",
		EnabledBundles: []string{"vikunja", "combo"},
	}); err != nil {
		t.Fatal(err)
	}

	addresses, err := SuggestSecretAddresses(projectRoot)
	if err != nil {
		t.Fatalf("SuggestSecretAddresses() = %v", err)
	}
	if len(addresses) != 1 || addresses[0] != "demo/private/local" {
		t.Fatalf("addresses = %#v, 期望只有本项目", addresses)
	}
}
