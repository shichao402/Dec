package app

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

func TestLoadAssetSelectionReturnsEnabledState(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/skills/project-workflow/SKILL.md": "---\nname: project-workflow\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "description: test\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"codex"},
		Editor:         "code --wait",
		EnabledBundles: []string{"default"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	state, err := LoadAssetSelection(projectRoot, nil)
	if err != nil {
		t.Fatalf("LoadAssetSelection() 失败: %v", err)
	}
	if !state.ExistingConfig {
		t.Fatal("应识别现有项目配置")
	}
	if len(state.Bundles) != 2 {
		t.Fatalf("Bundles = %d, 期望 2", len(state.Bundles))
	}
	if !bundleEnabledInState(state, "default") {
		t.Fatal("default bundle 应为启用态")
	}
	if bundleEnabledInState(state, "cli") {
		t.Fatal("cli bundle 未被引用，不应为启用态")
	}
}

func TestLoadAssetSelectionDiscoversBundlesWithoutProjectConfig(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	state, err := LoadAssetSelection(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("LoadAssetSelection() 失败: %v", err)
	}
	if state.ExistingConfig {
		t.Fatal("无 .dec/config.yaml 时不应标记 ExistingConfig")
	}
	if len(state.Bundles) < 2 {
		t.Fatalf("无项目配置时仍应发现 bundle, Bundles = %d", len(state.Bundles))
	}
	for _, bo := range state.Bundles {
		if bo.Enabled {
			t.Fatalf("无项目配置时不应有已启用 bundle: %s", bo.Name)
		}
	}
}

// TestSaveEnabledBundlesPreservesOtherFields 保证保存 bundle 勾选时，
// 未参与本次交互的字段（IDEs / Editor）原样保留。
func TestSaveEnabledBundlesPreservesOtherFields(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:   []string{"codex"},
		Editor: "code --wait",
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := SaveEnabledBundles(projectRoot, []string{"default"}, nil)
	if err != nil {
		t.Fatalf("SaveEnabledBundles() 失败: %v", err)
	}
	if result.EnabledBundleCount != 1 {
		t.Fatalf("EnabledBundleCount = %d, 期望 1", result.EnabledBundleCount)
	}
	if !result.VarsCreated {
		t.Fatal("首次保存应创建 vars 模板")
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if loaded.Editor != "code --wait" {
		t.Fatalf("Editor = %q, 期望 %q", loaded.Editor, "code --wait")
	}
	if !reflect.DeepEqual(loaded.IDEs, []string{"codex"}) {
		t.Fatalf("IDEs = %#v, 期望 %#v", loaded.IDEs, []string{"codex"})
	}
	if !reflect.DeepEqual(loaded.EnabledBundles, []string{"default"}) {
		t.Fatalf("EnabledBundles = %#v, 期望 [default]", loaded.EnabledBundles)
	}
}

// TestSaveEnabledBundlesNormalizes 保证传入列表会去重、去空白。
func TestSaveEnabledBundlesNormalizes(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{IDEs: []string{"codex"}}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := SaveEnabledBundles(projectRoot, []string{"combo", "  combo  ", "", "vikunja"}, nil)
	if err != nil {
		t.Fatalf("SaveEnabledBundles() 失败: %v", err)
	}
	if result.EnabledBundleCount != 2 {
		t.Fatalf("EnabledBundleCount = %d, 期望 2", result.EnabledBundleCount)
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if !reflect.DeepEqual(loaded.EnabledBundles, []string{"combo", "vikunja"}) {
		t.Fatalf("EnabledBundles = %#v, 期望 [combo vikunja]", loaded.EnabledBundles)
	}
}

// TestSaveEnabledBundlesEmptyPersistsNil 保证传入空列表时 EnabledBundles 清空为 nil，
// 便于 yaml omitempty 移除该键。
func TestSaveEnabledBundlesEmptyPersistsNil(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"codex"},
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	if _, err := SaveEnabledBundles(projectRoot, []string{}, nil); err != nil {
		t.Fatalf("SaveEnabledBundles() 失败: %v", err)
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if loaded.EnabledBundles != nil {
		t.Fatalf("EnabledBundles = %#v, 期望 nil", loaded.EnabledBundles)
	}
}

// TestSaveEnabledBundlesRoundTrip 覆盖「取消 bundle 后勾选自己弹回来」的回归点：
// 取消并保存后重新加载，该 bundle 不应再被判定为启用。
func TestSaveEnabledBundlesRoundTrip(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/vikunja/rules/vikunja-integration.mdc":    "---\ndescription: test\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"codex"},
		EnabledBundles: []string{"vikunja", "cli"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	state, err := LoadAssetSelection(projectRoot, nil)
	if err != nil {
		t.Fatalf("LoadAssetSelection() 失败: %v", err)
	}
	if !bundleEnabledInState(state, "vikunja") || !bundleEnabledInState(state, "cli") {
		t.Fatal("前置条件不成立：两个 bundle 初始都应为启用态")
	}

	if _, err := SaveEnabledBundles(projectRoot, []string{"cli"}, nil); err != nil {
		t.Fatalf("SaveEnabledBundles() 失败: %v", err)
	}

	reloaded, err := LoadAssetSelection(projectRoot, nil)
	if err != nil {
		t.Fatalf("重新 LoadAssetSelection() 失败: %v", err)
	}
	if bundleEnabledInState(reloaded, "vikunja") {
		t.Fatal("被取消的 vikunja 不应再是启用态")
	}
	if !bundleEnabledInState(reloaded, "cli") {
		t.Fatal("未被取消的 cli 应保持启用态")
	}
}

func TestListEffectiveEnabledAssetsExpandsBundleMembers(t *testing.T) {
	state := &AssetSelectionState{
		Bundles: []AssetBundleOption{
			{
				Name:    "vikunja",
				Enabled: true,
				Members: []AssetSelectionItem{
					{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja"},
					{Name: "vikunja-rules", Type: "rule", Vault: "vikunja"},
				},
			},
			{
				Name:    "cli",
				Enabled: false,
				Members: []AssetSelectionItem{
					{Name: "cli-release-rules", Type: "rule", Vault: "cli"},
				},
			},
		},
	}

	got := ListEffectiveEnabledAssets(state)
	if len(got) != 2 {
		t.Fatalf("有效启用资产数 = %d, 期望 2（仅 vikunja 成员）", len(got))
	}

	groups := ListEffectiveEnabledGroups(state)
	if len(groups) != 1 {
		t.Fatalf("分组数 = %d, 期望 1", len(groups))
	}
	if groups[0].Label != "bundle/vikunja" || len(groups[0].Items) != 2 {
		t.Fatalf("分组 = %#v, 期望 bundle/vikunja 含 2 项", groups[0])
	}
}

func TestListEffectiveEnabledAssetsDeduplicatesAcrossBundles(t *testing.T) {
	shared := AssetSelectionItem{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja"}
	state := &AssetSelectionState{
		Bundles: []AssetBundleOption{
			{Name: "vikunja", Enabled: true, Members: []AssetSelectionItem{shared}},
			{Name: "combo", Enabled: true, Members: []AssetSelectionItem{shared}},
		},
	}

	got := ListEffectiveEnabledAssets(state)
	if len(got) != 1 {
		t.Fatalf("去重后有效启用资产数 = %d, 期望 1", len(got))
	}
	if len(ListEffectiveEnabledGroups(state)) != 2 {
		t.Fatal("两个 bundle 都应各自成组")
	}
}

func TestLoadAssetSelectionDoesNotMergeUserEnabled(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/skills/project-workflow/SKILL.md": "---\nname: project-workflow\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "description: test\n",
		"bundles/cli/bundle.yaml":                          "name: cli\nscope: user\nmembers:\n  - rules/cli-release-rules\n",
		"bundles/default/bundle.yaml":                      "name: default\nscope: project\nmembers:\n  - skills/project-workflow\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote, EnabledBundles: []string{"cli"}}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

	state, err := LoadAssetSelection(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("LoadAssetSelection() 失败: %v", err)
	}
	if bundleEnabledInState(state, "cli") {
		t.Fatal("cli 为 scope:user，项目平面 Bundles 列表不应启用它")
	}
	for _, bo := range state.Bundles {
		if bo.Name == "cli" {
			t.Fatal("项目平面不应列出 scope:user 的 cli bundle")
		}
	}
}

func TestSaveWorkspaceEnabledBundlesUserWritesGlobalConfig(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/cli/bundle.yaml": "name: cli\nscope: user\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	if _, err := SaveWorkspaceEnabledBundles(NewWorkspace(WorkspaceUser, ""), []string{"cli"}, nil); err != nil {
		t.Fatalf("SaveWorkspaceEnabledBundles(user) 失败: %v", err)
	}
	global, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(global.EnabledBundles, []string{"cli"}) {
		t.Fatalf("GlobalConfig.EnabledBundles = %#v", global.EnabledBundles)
	}
}

func bundleEnabledInState(state *AssetSelectionState, name string) bool {
	for _, bo := range state.Bundles {
		if bo.Name == name && bo.Enabled {
			return true
		}
	}
	return false
}

func TestLoadAssetSelection_IncludesRemoteSecretMembersAndCaches(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/agents-board/skills/board/SKILL.md": "---\nname: board\n---\n",
		"bundles/agents-board/bundle.yaml":           "name: agents-board\nscope: project\nmembers:\n  - skill/board\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"agents-board/private/project": {{RelativePath: ".env/foo.env"}},
			},
			SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
				"agents-board/private/project": {{Name: ".sshkey/deploy"}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	state, err := LoadAssetSelection(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("LoadAssetSelection() = %v", err)
	}
	bo := bundleOptionByName(t, state, "agents-board")
	if len(bo.Members) != 3 {
		t.Fatalf("members = %#v, 期望 vault skill + 2 secrets", bo.Members)
	}
	if !hasMember(bo, "skill", "board") || !hasMember(bo, AssetMemberTypeSecret, ".env/foo.env") || !hasMember(bo, AssetMemberTypeSecret, ".sshkey/deploy") {
		t.Fatalf("应包含公开成员与 secrets 路径: %#v", bo.Members)
	}
	cached := secrets.SecretBundleMembers("agents-board")
	if len(cached) != 2 || cached[0] != ".env/foo.env" || cached[1] != ".sshkey/deploy" {
		t.Fatalf("应写入 known_secret_bundle_members: %#v", cached)
	}
}

func TestLoadAssetSelection_UsesCachedSecretMembersWithoutSession(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/agents-board/skills/board/SKILL.md": "---\nname: board\n---\n",
		"bundles/agents-board/bundle.yaml":           "name: agents-board\nscope: project\nmembers:\n  - skill/board\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := secrets.RememberSecretBundleMembers("agents-board", []string{".env/foo.env", ".sshkey/deploy"}); err != nil {
		t.Fatal(err)
	}

	state, err := LoadAssetSelection(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("LoadAssetSelection() = %v", err)
	}
	bo := bundleOptionByName(t, state, "agents-board")
	if !hasMember(bo, AssetMemberTypeSecret, ".env/foo.env") || !hasMember(bo, AssetMemberTypeSecret, ".sshkey/deploy") {
		t.Fatalf("无 session 时应回填缓存成员: %#v", bo.Members)
	}
}

func TestLoadWorkspaceAssetSelection_SecretsOnlyShowsCachedMembers(t *testing.T) {
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

	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			SecretBundleFolders: []string{"pkv"},
			NotesByFolder: map[string][]secrets.SecureNote{
				"pkv/private/user": {{RelativePath: ".env/pkv.env"}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	state, err := LoadWorkspaceAssetSelection(NewWorkspace(WorkspaceUser, ""), nil)
	if err != nil {
		t.Fatalf("LoadWorkspaceAssetSelection() = %v", err)
	}
	bo := bundleOptionByName(t, state, "pkv")
	if !bo.SecretsOnly {
		t.Fatalf("pkv 应为 SecretsOnly: %#v", bo)
	}
	if !hasMember(bo, AssetMemberTypeSecret, ".env/pkv.env") {
		t.Fatalf("SecretsOnly 也应列出 secrets 成员: %#v", bo.Members)
	}
}

func TestListEffectiveEnabledAssets_SkipsSecretMembers(t *testing.T) {
	state := &AssetSelectionState{
		Bundles: []AssetBundleOption{
			{
				Name:    "vikunja",
				Enabled: true,
				Members: []AssetSelectionItem{
					{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja"},
					{Name: ".env/vikunja.env", Type: AssetMemberTypeSecret, Vault: "vikunja"},
				},
			},
		},
	}
	got := ListEffectiveEnabledAssets(state)
	if len(got) != 1 || got[0].Type != "skill" {
		t.Fatalf("pull 目标集不应含 secrets 展示项: %#v", got)
	}
}

func bundleOptionByName(t *testing.T, state *AssetSelectionState, name string) AssetBundleOption {
	t.Helper()
	for _, bo := range state.Bundles {
		if bo.Name == name {
			return bo
		}
	}
	t.Fatalf("找不到 bundle %q: %#v", name, state.Bundles)
	return AssetBundleOption{}
}

func hasMember(bo AssetBundleOption, itemType, name string) bool {
	for _, item := range bo.Members {
		if item.Type == itemType && item.Name == name {
			return true
		}
	}
	return false
}
