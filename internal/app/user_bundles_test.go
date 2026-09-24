package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

// 拒绝项不能留在 requires 里。
func TestSetWorkspaceRequires_ExcludesRejectedFromUserConfig(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"cli/dec.yaml": "name: cli\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote, RegistryURL: remote}); err != nil {
		t.Fatal(err)
	}

	result, err := SetWorkspaceRequires(
		context.Background(),
		NewWorkspace(WorkspaceUser, ""),
		types.RequiresSpec{"cli": types.RequiresLatest, "vikunja": types.RequiresVault},
		nil,
	)
	if err != nil {
		t.Fatalf("SetWorkspaceRequires() = %v", err)
	}
	if len(result.Subscribed) != 1 || result.Subscribed[0].Project != "cli" {
		t.Fatalf("Subscribed = %#v, 期望仅 cli", result.Subscribed)
	}
	if len(result.Rejected) != 1 || !strings.Contains(result.Rejected[0], "vikunja") {
		t.Fatalf("应报告被拒的 vikunja: %#v", result.Rejected)
	}

	saved, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Requires.OfficialProjects()) != 1 || saved.Requires["cli"] != types.RequiresLatest {
		t.Fatalf("Requires = %#v, 期望仅 cli: latest", saved.Requires)
	}
}

// ADR 0012：用户平面 Bundles 页是启用列表的唯一入口，因此候选必须包含只存在于
// Bitwarden / known 的 bundle，否则 secrets-only bundle 无法被首次勾选。
func TestLoadWorkspaceAssetSelection_UserPlaneIncludesSecretsOnlyCandidates(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"cli/dec.yaml": "name: cli\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote, Requires: types.RequiresSpec{"cli": types.RequiresLatest}}); err != nil {
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
		"cli/dec.yaml": "name: cli\n",
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
		"cli/dec.yaml": "name: cli\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote, Requires: types.RequiresSpec{"vikunja": types.RequiresLatest}}); err != nil {
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
		"cli/dec.yaml": "name: cli\n",
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

// 平面隔离（ADR 0009）：project 上下文的 pull 不再并入用户平面启用列表。
func TestPullProjectAssets_IgnoresUserEnabledBundles(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"cli/public/project/skills/cli-skill/SKILL.md": "---\nname: cli-skill\n---\n",
		"cli/dec.yaml": `name: cli
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote, Requires: types.RequiresSpec{"cli": types.RequiresVault}}); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:     []string{"cursor"},
		Requires: nil,
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
	if result.SkippedReason != "未订阅任何项目" {
		t.Fatalf("SkippedReason = %q", result.SkippedReason)
	}
}
