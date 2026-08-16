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

// 远端 vault 删了成员 + Bitwarden 删了 Note：pull 后本地 cache/IDE/secrets 孤儿被清。
func TestPullReconcile_CleansRemoteDeletedOrphans(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	home := useTempHomeForSSH(t)

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/pkv/skills/keep-skill/SKILL.md": "---\nname: keep-skill\n---\n",
		"bundles/pkv/bundle.yaml": `name: pkv
members:
  - skill/keep-skill
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "Demo",
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"pkv"},
	}); err != nil {
		t.Fatal(err)
	}

	orphanCache := filepath.Join(projectRoot, ".dec", "cache", "pkv", "skills", "gone-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(orphanCache), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphanCache, []byte("---\nname: gone-skill\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	orphanIDE := filepath.Join(projectRoot, ".cursor", "skills", "dec-gone-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(orphanIDE), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphanIDE, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	keepNote := filepath.Join(projectRoot, ".secrets", "bundles", "pkv", ".env", "keep.env")
	goneNote := filepath.Join(projectRoot, ".secrets", "bundles", "pkv", ".env", "gone.env")
	if err := os.MkdirAll(filepath.Dir(keepNote), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepNote, []byte("K=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goneNote, []byte("G=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	landings, err := secrets.PrepareSSHKeyLandings("pkv", []secrets.SSHKeyItem{
		{Name: ".sshkey/orphan", PrivateKey: "priv\n", PublicKey: "pub\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.WriteSSHKeyLandings(landings); err != nil {
		t.Fatal(err)
	}
	orphanSSH := filepath.Join(home, ".ssh", "dec_pkv_orphan")
	if _, err := os.Stat(orphanSSH); err != nil {
		t.Fatalf("预置 SSH 失败: %v", err)
	}

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"bundle/pkv": {{RelativePath: ".env/keep.env", Content: "K=1\n"}},
				"Demo":       {},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() = %v", err)
	}
	if _, err := os.Stat(orphanCache); !os.IsNotExist(err) {
		t.Fatalf("vault 已无成员的 cache 应清理, err=%v", err)
	}
	if _, err := os.Stat(orphanIDE); !os.IsNotExist(err) {
		t.Fatalf("vault 已无成员的 IDE 安装应清理, err=%v", err)
	}
	if _, err := os.Stat(goneNote); !os.IsNotExist(err) {
		t.Fatalf("远端已删 Note 应清理, err=%v", err)
	}
	if _, err := os.Stat(keepNote); err != nil {
		t.Fatalf("远端仍在的 Note 应保留: %v", err)
	}
	if _, err := os.Stat(orphanSSH); !os.IsNotExist(err) {
		t.Fatalf("远端已删 SSH 应清理, err=%v", err)
	}
	if len(result.CleanedAssets) == 0 && len(result.OrphanSecretPaths)+len(result.OrphanSSHKeys) == 0 {
		t.Fatalf("应报告孤儿清理: CleanedAssets=%#v secrets=%#v ssh=%#v",
			result.CleanedAssets, result.OrphanSecretPaths, result.OrphanSSHKeys)
	}
}

// 未启用包：本地 secrets 不得因本次 pull 被清。
func TestPullReconcile_DoesNotTouchDisabledBundleSecrets(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/active/skills/active-skill/SKILL.md": "---\nname: active-skill\n---\n",
		"bundles/active/bundle.yaml": `name: active
members:
  - skill/active-skill
`,
		"bundles/parked/skills/parked-skill/SKILL.md": "---\nname: parked-skill\n---\n",
		"bundles/parked/bundle.yaml": `name: parked
members:
  - skill/parked-skill
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"active"},
	}); err != nil {
		t.Fatal(err)
	}

	parkedSecret := filepath.Join(projectRoot, ".secrets", "bundles", "parked", "env", "x.env")
	if err := os.MkdirAll(filepath.Dir(parkedSecret), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parkedSecret, []byte("X=1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"bundle/active": {{RelativePath: ".env/a.env", Content: "A=1\n"}},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() = %v", err)
	}
	if _, err := os.Stat(parkedSecret); err != nil {
		t.Fatalf("未启用包的 secrets 不应被清: %v", err)
	}
	for _, p := range result.OrphanSecretPaths {
		if strings.Contains(p, "parked") {
			t.Fatalf("不应清理停用包 secrets: %s", p)
		}
	}
}

// 未能确认 Bitwarden（未配置）时：vault 缺失可摘 enabled，但 secrets 只报告不删。
func TestPullReconcile_ReportsSecretsWhenBWUnconfirmed(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/skills/ok/SKILL.md": "---\nname: ok\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"pkv"},
	}); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(projectRoot, ".secrets", "bundles", "pkv", "env", "x.env")
	if err := os.MkdirAll(filepath.Dir(secretPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("X=1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() = %v", err)
	}
	if _, err := os.Stat(secretPath); err != nil {
		t.Fatalf("未能确认 Bitwarden 时不应删除 secrets: %v", err)
	}
	if len(result.OrphanReportedOnly) == 0 {
		t.Fatalf("应报告未删残留: %#v", result.OrphanReportedOnly)
	}
	updated, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range updated.EnabledBundles {
		if b == "pkv" {
			t.Fatalf("vault 已确认缺失时应从 enabled 摘掉 pkv: %#v", updated.EnabledBundles)
		}
	}
}

// 用户平面只清机器 secrets 根，不动项目平面 .secrets。
func TestPullReconcile_UserPlaneOnlyCleansMachineSecrets(t *testing.T) {
	decHome := setupSecretsConfigForPushTest(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/woa/bundle.yaml": "name: woa\nscope: user\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{
		RepoURL:        remote,
		EnabledBundles: []string{"woa"},
		IDEs:           []string{"cursor"},
	}); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	projectSecret := filepath.Join(projectRoot, ".secrets", "bundles", "woa", "env", "proj.env")
	if err := os.MkdirAll(filepath.Dir(projectSecret), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectSecret, []byte("P=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	machineSecret := filepath.Join(decHome, "secrets", "bundles", "woa", "env", "m.env")
	if err := os.MkdirAll(filepath.Dir(machineSecret), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(machineSecret, []byte("M=1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"bundle/woa": {},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	result, err := PullWorkspaceAssets(context.Background(), NewWorkspace(WorkspaceUser, projectRoot), "", nil)
	if err != nil {
		t.Fatalf("PullWorkspaceAssets(user) = %v", err)
	}
	if _, err := os.Stat(machineSecret); !os.IsNotExist(err) {
		t.Fatalf("用户平面应清理机器 secrets 孤儿, err=%v result=%#v", err, result.OrphanSecretPaths)
	}
	if _, err := os.Stat(projectSecret); err != nil {
		t.Fatalf("用户平面 pull 不得动项目 .secrets: %v", err)
	}
}

// vault 整包已删且 Bitwarden 确认空：收敛 enabled + 清本平面 secrets。
func TestPullReconcile_MissingVaultBundleFullCleanup(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/skills/ok/SKILL.md": "---\nname: ok\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "Demo",
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"pkv"},
	}); err != nil {
		t.Fatal(err)
	}
	secretDir := filepath.Join(projectRoot, ".secrets", "bundles", "pkv", "env")
	if err := os.MkdirAll(secretDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretDir, "x.env"), []byte("X=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := secrets.RememberSecretBundles([]string{"pkv"}); err != nil {
		t.Fatal(err)
	}

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"bundle/pkv": {},
			"Demo":       {},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() = %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "bundles", "pkv")); !os.IsNotExist(err) {
		t.Fatalf("vault+BW 均确认后应删 secrets 同步根, err=%v", err)
	}
	updated, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range updated.EnabledBundles {
		if b == "pkv" {
			t.Fatalf("应摘除 enabled pkv: %#v", updated.EnabledBundles)
		}
	}
	cfg, err := secrets.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range cfg.KnownSecretBundleNames() {
		if name == "pkv" {
			t.Fatalf("应从 known 摘除 pkv: %#v", cfg.KnownSecretBundleNames())
		}
	}
	if len(result.OrphanClearedBundles) == 0 {
		t.Fatalf("应记录 ClearedBundles: %#v", result)
	}
}
