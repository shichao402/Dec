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

	repair, err := ensureVaultBundlesForUserEnable([]string{"cli", "woa"}, nil)
	if err != nil {
		t.Fatalf("ensureVaultBundlesForUserEnable() = %v", err)
	}
	if len(repair.Created) != 1 || repair.Created[0] != "woa" {
		t.Fatalf("created = %#v, 期望 [woa]", repair.Created)
	}
	if len(repair.Rejected) != 0 {
		t.Fatalf("无 project 引用且缺省 scope 不应被拒: %#v", repair.Rejected)
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

// ADR 0013：显式 scope: project 是 manifest 作者的明确声明，勾选用户平面不得静默改写它，
// 否则所有引用该 bundle 的 project 会因平面隔离突然拉不到资产。
func TestEnsureVaultBundlesForUserEnable_RejectsExplicitProjectScope(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/cli/bundle.yaml": "name: cli\nscope: project\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	repair, err := ensureVaultBundlesForUserEnable([]string{"cli"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repair.Created) != 0 || len(repair.Upgraded) != 0 {
		t.Fatalf("不应创建或升级: %#v", repair)
	}
	if len(repair.Rejected) != 1 || repair.Rejected[0].Name != "cli" {
		t.Fatalf("应拒绝 cli: %#v", repair.Rejected)
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
	if !strings.Contains(string(data), "scope: project") {
		t.Fatalf("manifest 不应被改写, 实际: %s", data)
	}
}

// 缺省 scope 允许按 ADR 0009 迁移期推断，但前提是没有 project 还在引用它。
func TestEnsureVaultBundlesForUserEnable_RejectsDefaultScopeUsedByProject(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/bundle.yaml": "name: vikunja\nmembers: []\n",
		"projects/Dec.yaml":           "name: Dec\nbundles:\n  - vikunja\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	repair, err := ensureVaultBundlesForUserEnable([]string{"vikunja"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repair.Upgraded) != 0 {
		t.Fatalf("被 project 引用的包不应升级: %#v", repair)
	}
	if len(repair.Rejected) != 1 || !strings.Contains(repair.Rejected[0].Reason, "Dec") {
		t.Fatalf("拒绝原因应点明引用它的 project: %#v", repair.Rejected)
	}

	tx, err := repo.NewReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	data, err := os.ReadFile(filepath.Join(tx.WorkDir(), "bundles", "vikunja", "bundle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "scope: user") {
		t.Fatalf("manifest 不应被改写为 user, 实际: %s", data)
	}
}

// 拒绝项不能留在 enabled_bundles 里，否则会是一个「勾了但平面隔离永远看不见」的条目。
func TestSaveWorkspaceEnabledBundles_ExcludesRejectedFromUserConfig(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/cli/bundle.yaml":     "name: cli\nscope: user\nmembers: []\n",
		"bundles/vikunja/bundle.yaml": "name: vikunja\nscope: project\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote}); err != nil {
		t.Fatal(err)
	}

	result, err := SaveWorkspaceEnabledBundles(NewWorkspace(WorkspaceUser, ""), []string{"cli", "vikunja"}, nil)
	if err != nil {
		t.Fatalf("SaveWorkspaceEnabledBundles() = %v", err)
	}
	if result.EnabledBundleCount != 1 {
		t.Fatalf("EnabledBundleCount = %d, 期望 1", result.EnabledBundleCount)
	}
	if len(result.RejectedBundles) != 1 || !strings.Contains(result.RejectedBundles[0], "vikunja") {
		t.Fatalf("应报告被拒的 vikunja: %#v", result.RejectedBundles)
	}

	saved, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.EnabledBundles) != 1 || saved.EnabledBundles[0] != "cli" {
		t.Fatalf("enabled_bundles = %#v, 期望仅 [cli]", saved.EnabledBundles)
	}
}

// ADR 0012：用户平面 Bundles 页是启用列表的唯一入口，因此候选必须包含只存在于
// Bitwarden / known 的 bundle，否则 secrets-only bundle 无法被首次勾选。
func TestLoadWorkspaceAssetSelection_UserPlaneIncludesSecretsOnlyCandidates(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/cli/bundle.yaml": "name: cli\nscope: user\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote, EnabledBundles: []string{"cli"}}); err != nil {
		t.Fatal(err)
	}

	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{SecretBundleFolders: []string{"vikunja"}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	state, err := LoadWorkspaceAssetSelection(NewWorkspace(WorkspaceUser, ""), nil)
	if err != nil {
		t.Fatalf("LoadWorkspaceAssetSelection() = %v", err)
	}

	byName := make(map[string]AssetBundleOption, len(state.Bundles))
	for _, bo := range state.Bundles {
		byName[bo.Name] = bo
	}
	cli, ok := byName["cli"]
	if !ok || cli.SecretsOnly || !cli.Enabled {
		t.Fatalf("vault bundle cli 应为已启用的普通条目: %#v", cli)
	}
	vikunja, ok := byName["vikunja"]
	if !ok {
		t.Fatalf("仅 Bitwarden 存在的 bundle 应出现在候选中: %#v", state.Bundles)
	}
	if !vikunja.SecretsOnly || vikunja.Enabled {
		t.Fatalf("vikunja 应标记 SecretsOnly 且未启用: %#v", vikunja)
	}
}

// known_secret_bundles 只增不减，远端删掉 folder 后名字会一直留着。远端核对成功且没人启用时，
// 这类残留必须被摘掉——留着会以「Bitwarden 已有同名 secrets」的面目诱导用户勾选空 bundle。
func TestLoadWorkspaceAssetSelection_PrunesKnownBundleMissingOnRemote(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/cli/bundle.yaml": "name: cli\nscope: user\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote}); err != nil {
		t.Fatal(err)
	}
	if err := secrets.RememberSecretBundles([]string{"woa", "vikunja"}); err != nil {
		t.Fatal(err)
	}

	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{SecretBundleFolders: []string{"woa"}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	state, err := LoadWorkspaceAssetSelection(NewWorkspace(WorkspaceUser, ""), nil)
	if err != nil {
		t.Fatalf("LoadWorkspaceAssetSelection() = %v", err)
	}
	byName := make(map[string]AssetBundleOption, len(state.Bundles))
	for _, bo := range state.Bundles {
		byName[bo.Name] = bo
	}
	woa, ok := byName["woa"]
	if !ok || !woa.SecretsOnly || woa.RemoteMissing || woa.RemoteUnverified {
		t.Fatalf("远端存在的 woa 应是已核对的 SecretsOnly 候选: %#v", woa)
	}
	if _, ok := byName["vikunja"]; ok {
		t.Fatalf("远端已无、也没人启用的 vikunja 不应留在候选里: %#v", state.Bundles)
	}

	cfg, err := secrets.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range cfg.KnownSecretBundleNames() {
		if name == "vikunja" {
			t.Fatalf("vikunja 应已从 known_secret_bundles 摘除: %#v", cfg.KnownSecretBundleNames())
		}
	}
}

// 已启用但远端无内容的 bundle 不能摘（用户还得能取消勾选），但必须如实标注，
// 不能继续声称「Bitwarden 里已有同名 secrets」。
func TestLoadWorkspaceAssetSelection_MarksEnabledBundleMissingOnRemote(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/cli/bundle.yaml": "name: cli\nscope: user\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote, EnabledBundles: []string{"vikunja"}}); err != nil {
		t.Fatal(err)
	}

	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	state, err := LoadWorkspaceAssetSelection(NewWorkspace(WorkspaceUser, ""), nil)
	if err != nil {
		t.Fatalf("LoadWorkspaceAssetSelection() = %v", err)
	}
	for _, bo := range state.Bundles {
		if bo.Name != "vikunja" {
			continue
		}
		if !bo.Enabled || !bo.SecretsOnly || !bo.RemoteMissing {
			t.Fatalf("已启用但远端无内容应标 RemoteMissing: %#v", bo)
		}
		return
	}
	t.Fatalf("已启用的 vikunja 应留在候选里: %#v", state.Bundles)
}

// 无 session / 枚举失败时名单为空，不能当成「远端没有」：既不摘 known，也不下结论。
func TestLoadWorkspaceAssetSelection_MarksRemoteUnverifiedWithoutSession(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/cli/bundle.yaml": "name: cli\nscope: user\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote}); err != nil {
		t.Fatal(err)
	}
	if err := secrets.RememberSecretBundles([]string{"vikunja"}); err != nil {
		t.Fatal(err)
	}

	state, err := LoadWorkspaceAssetSelection(NewWorkspace(WorkspaceUser, ""), nil)
	if err != nil {
		t.Fatalf("LoadWorkspaceAssetSelection() = %v", err)
	}
	var found bool
	for _, bo := range state.Bundles {
		if bo.Name != "vikunja" {
			continue
		}
		found = true
		if !bo.SecretsOnly || !bo.RemoteUnverified || bo.RemoteMissing {
			t.Fatalf("未核对远端时应标 RemoteUnverified: %#v", bo)
		}
	}
	if !found {
		t.Fatalf("未核对远端时不应摘掉候选: %#v", state.Bundles)
	}
	cfg, err := secrets.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	var kept bool
	for _, name := range cfg.KnownSecretBundleNames() {
		if name == "vikunja" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("未核对远端时不应摘 known_secret_bundles: %#v", cfg.KnownSecretBundleNames())
	}
}

// ADR 0013：known_secret_bundles 混着两平面的名字。vault 里已有 manifest、只是 scope 属于
// 另一平面的条目必须标 OtherPlane——标成 SecretsOnly 会谎称「vault 尚无 manifest」，
// 并诱导用户勾选，进而触发跨平面 scope 改写。
func TestLoadWorkspaceAssetSelection_MarksOtherPlaneInsteadOfSecretsOnly(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/bundle.yaml": "name: vikunja\nscope: project\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote}); err != nil {
		t.Fatal(err)
	}
	if err := secrets.RememberSecretBundles([]string{"vikunja"}); err != nil {
		t.Fatal(err)
	}

	state, err := LoadWorkspaceAssetSelection(NewWorkspace(WorkspaceUser, ""), nil)
	if err != nil {
		t.Fatalf("LoadWorkspaceAssetSelection() = %v", err)
	}
	var found bool
	for _, bo := range state.Bundles {
		if bo.Name != "vikunja" {
			continue
		}
		found = true
		if bo.SecretsOnly {
			t.Fatalf("vault 已有 manifest，不应标 SecretsOnly: %#v", bo)
		}
		if !bo.OtherPlane {
			t.Fatalf("应标 OtherPlane: %#v", bo)
		}
	}
	if !found {
		t.Fatalf("候选中应出现 vikunja: %#v", state.Bundles)
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
