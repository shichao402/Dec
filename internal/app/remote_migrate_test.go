package app

import (
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

func TestMigrateUnmanagedNoteToBundle_MovesAndEnables(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"projects/Dec.yaml": "name: Dec\nbundles: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote}); err != nil {
		t.Fatal(err)
	}

	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"Dec": {{RelativePath: ".gcm/cnb.yaml", Content: "host: cnb.cool\nusername: firo\npassword: tok\n"}},
	}}
	oldFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = oldFactory })

	result, err := MigrateUnmanagedNoteToBundle(context.Background(), MigrateUnmanagedNoteInput{
		SourceFolder: "Dec",
		NotePath:     ".gcm/cnb.yaml",
		DestBundle:   "cnb",
		Plane:        WorkspaceUser,
		Enable:       true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.DestFolder != "bundle/cnb" || result.SourceFolder != "Dec" {
		t.Fatalf("result = %#v", result)
	}
	if len(stub.NotesByFolder["Dec"]) != 0 {
		t.Fatalf("源 folder 应已删除 note: %#v", stub.NotesByFolder["Dec"])
	}
	if len(stub.NotesByFolder["bundle/cnb"]) != 1 {
		t.Fatalf("目标 folder = %#v", stub.NotesByFolder["bundle/cnb"])
	}
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.EnabledBundles) != 1 || cfg.EnabledBundles[0] != "cnb" {
		t.Fatalf("enabled = %#v", cfg.EnabledBundles)
	}
	tx, err := repo.NewReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	data, err := os.ReadFile(filepath.Join(tx.WorkDir(), "bundles", "cnb", "bundle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "scope: user") {
		t.Fatalf("应创建 user 占位: %s", data)
	}
}

func TestMigrateUnmanagedNoteToBundle_RejectsBundleSource(t *testing.T) {
	_, err := MigrateUnmanagedNoteToBundle(context.Background(), MigrateUnmanagedNoteInput{
		SourceFolder: "bundle/cnb",
		NotePath:     ".gcm/cnb.yaml",
		DestBundle:   "cnb",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "无需") {
		t.Fatalf("应拒绝已是 bundle/* 的源: %v", err)
	}
}

func TestMigrateProjectSecretsToBundle_MovesNotesAndLocalTree(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"projects/Dec.yaml": "name: Dec\nbundles: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote}); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	localSrc := filepath.Join(projectRoot, ".secrets", "project")
	if err := os.MkdirAll(localSrc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localSrc, "token.txt"), []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "Dec"}); err != nil {
		t.Fatal(err)
	}

	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"Dec": {{RelativePath: "token.txt", Content: "secret"}},
	}}
	oldFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = oldFactory })

	result, err := MigrateProjectSecretsToBundle(context.Background(), MigrateProjectSecretsInput{
		ProjectRoot:  projectRoot,
		ProjectName:  "Dec",
		DeleteSource: true,
		Enable:       true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.DestFolder != "bundle/Dec" || len(result.MigratedNotes) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(stub.NotesByFolder["Dec"]) != 0 {
		t.Fatalf("源应清空: %#v", stub.NotesByFolder["Dec"])
	}
	if len(stub.NotesByFolder["bundle/Dec"]) != 1 {
		t.Fatalf("目标 = %#v", stub.NotesByFolder["bundle/Dec"])
	}
	destLocal := filepath.Join(projectRoot, ".secrets", "bundles", "Dec", "token.txt")
	if _, err := os.Stat(destLocal); err != nil {
		t.Fatalf("本地未迁到 bundles: %v", err)
	}
	pc, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(pc.EnabledBundles) != 1 || pc.EnabledBundles[0] != "Dec" {
		t.Fatalf("enabled = %#v", pc.EnabledBundles)
	}
}
