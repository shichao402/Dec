package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/handler"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

func TestPullProjectAssets_UsesDefaultServerWithoutConfigFile(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/skills/project-workflow/SKILL.md": "---\nname: project-workflow\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"default"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	var events []OperationEvent
	result, err := PullProjectAssets(context.Background(), projectRoot, "", ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.PulledCount != 1 {
		t.Fatalf("PulledCount = %d, 期望 1", result.PulledCount)
	}
	if containsScopeMessage(events, "pull.secrets", "Bitwarden 未配置") {
		t.Fatalf("默认 server_url 时不应跳过 secrets: %#v", events)
	}
}

func TestPullProjectAssets_RejectsSecretsOverlap(t *testing.T) {
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

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"bundle/combo": {{
					RelativePath: ".dec/cache/combo/skills/bundle-skill/SKILL.md",
					Content:      "secret",
				}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/combo/skills/bundle-skill/SKILL.md": "---\nname: bundle-skill\n---\n",
		"bundles/combo/bundle.yaml": `name: combo
members:
  - skill/bundle-skill
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("secrets 冲突应为部分成功（公开资产保留）: %v", err)
	}
	if result == nil || len(result.NonFatalWarnings) == 0 {
		t.Fatal("期望 NonFatalWarnings 记录 secrets 冲突")
	}
	joined := strings.Join(result.NonFatalWarnings, "\n")
	if !strings.Contains(joined, "冲突") && !strings.Contains(joined, "未知点类型目录") {
		t.Fatalf("警告应描述路径冲突或非法点目录: %v", result.NonFatalWarnings)
	}

	// secrets 在公开资产之后执行：重叠校验只拦下密文落地，不回滚已就绪的 Dec 资产。
	cached, readErr := os.ReadFile(filepath.Join(projectRoot, ".dec", "cache", "combo", "skills", "bundle-skill", "SKILL.md"))
	if readErr != nil {
		t.Fatalf("Dec 资产缓存应存在: %v", readErr)
	}
	if strings.Contains(string(cached), "secret") {
		t.Fatalf("重叠的密文不应覆盖 Dec 资产: %s", cached)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-bundle-skill", "SKILL.md")); err != nil {
		t.Fatalf("公开资产应已安装: %v", err)
	}
}

// secrets 失败（未解锁 / 网络故障）不应让已就绪的公开资产落空。
func TestPullProjectAssets_InstallsAssetsWhenSecretsFail(t *testing.T) {
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
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"cli"},
	}); err != nil {
		t.Fatal(err)
	}

	// 无 session：secrets 阶段会尝试 web unlock，测试环境下被 guard 拒绝。
	secrets.ClearSession()

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("secrets 失败应为部分成功: %v", err)
	}
	if result == nil || len(result.NonFatalWarnings) == 0 {
		t.Fatal("期望 NonFatalWarnings 记录解锁失败")
	}

	installed := filepath.Join(projectRoot, ".cursor", "skills", "dec-cli-skill", "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("secrets 失败不应阻断 IDE 安装: %v", err)
	}
}

func containsScopeMessage(events []OperationEvent, scope, fragment string) bool {
	for _, event := range events {
		if event.Scope == scope && strings.Contains(event.Message, fragment) {
			return true
		}
	}
	return false
}

// 远端仍有的 Note 在 pull 后保留；远端已删的 Note 会被 prune。
// 停用 bundle 的本地 secrets 不在本次 SyncTarget 内，不会被误清。
func TestPullEnabledSecretsBundles_PrunesRemoteDeletedKeepsPresent(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"bundle/Demo": {{RelativePath: "config/server.yaml", Content: "keep: true\n"}},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "Demo",
		EnabledBundles: []string{"Demo"},
	}); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(projectRoot, ".secrets", "bundles", "Demo", "config", "server.yaml")
	gone := filepath.Join(projectRoot, ".secrets", "bundles", "Demo", "config", "orphan.yaml")
	if err := os.MkdirAll(filepath.Dir(keep), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("keep: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gone, []byte("orphan: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// 停用包本地残留：不在 pull 范围，不得误删。
	disabled := filepath.Join(projectRoot, ".secrets", "bundles", "disabled", ".env", "x.env")
	if err := os.MkdirAll(filepath.Dir(disabled), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disabled, []byte("X=1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	summary, err := pullEnabledSecretsBundles(context.Background(), projectRoot, []string{"Demo"}, nil)
	if err != nil {
		t.Fatalf("pullEnabledSecretsBundles() 失败: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("远端仍在的 Note 应保留: %v", err)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatalf("远端已删的 Note 应被 prune, err=%v", err)
	}
	if _, err := os.Stat(disabled); err != nil {
		t.Fatalf("未启用 bundle 的本地 secrets 不应被清: %v", err)
	}
	if len(summary.Orphans.RemovedSecretPaths) == 0 {
		t.Fatalf("应报告清理了孤儿 Note: %#v", summary.Orphans)
	}
}

func TestPullEnabledSecretsBundles_RejectsSecretsDecOverlap(t *testing.T) {
	setupSecretsConfigForPushTest(t)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"bundle/combo": {{RelativePath: ".dec/embedded/secret.env", Content: "secret"}},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := pullEnabledSecretsBundles(context.Background(), projectRoot, []string{"combo"}, nil)
	if err == nil {
		t.Fatal("secrets 路径与 .dec/ 相交时应报错")
	}
	if !strings.Contains(err.Error(), "冲突") && !strings.Contains(err.Error(), "未知点类型目录") {
		t.Fatalf("错误应描述冲突或非法点目录: %v", err)
	}
}

func useTempHomeForSSH(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestPullEnabledSecretsBundles_MixedNotesAndSSHKeys(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	home := useTempHomeForSSH(t)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"bundle/vikunja": {{RelativePath: ".env/vikunja.env", Content: "VIKUNJA_API_TOKEN=abc\n"}},
			},
			SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
				"bundle/vikunja": {{
					Name: ".sshkey/deploy", Hosts: []string{"vikunja.example.com"},
					PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nPRIV\n-----END OPENSSH PRIVATE KEY-----\n",
					PublicKey:  "ssh-ed25519 AAAA deploy\n",
				}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{EnabledBundles: []string{"vikunja"}}); err != nil {
		t.Fatal(err)
	}

	summary, err := pullEnabledSecretsBundles(context.Background(), projectRoot, []string{"vikunja"}, nil)
	if err != nil {
		t.Fatalf("pullEnabledSecretsBundles() = %v", err)
	}
	if summary.NoteCount != 1 || summary.SSHKeyCount != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "bundles", "vikunja", ".env", "vikunja.env")); err != nil {
		t.Fatalf("Secure Note 应落地: %v", err)
	}
	priv := filepath.Join(home, ".ssh", "dec_vikunja_deploy")
	if _, err := os.Stat(priv); err != nil {
		t.Fatalf("SSH 私钥应落地: %v", err)
	}
	// managed Host 落在 ~/.ssh/config.d/dec.conf，主 config 只置顶 Include。
	cfgRaw, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgRaw), "Include config.d/dec.conf") {
		t.Fatalf("主 SSH config 应 Include managed 文件:\n%s", cfgRaw)
	}
	managedRaw, err := os.ReadFile(filepath.Join(home, ".ssh", "config.d", "dec.conf"))
	if err != nil {
		t.Fatalf("managed SSH config 应落地: %v", err)
	}
	if !strings.Contains(string(managedRaw), "vikunja.example.com") {
		t.Fatalf("managed SSH config 应含 host:\n%s", managedRaw)
	}
}

func TestPullEnabledSecretsBundles_SSHValidationFailureWritesNothing(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	home := useTempHomeForSSH(t)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"bundle/vikunja": {{RelativePath: ".env/vikunja.env", Content: "TOKEN=1\n"}},
			},
			SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
				"bundle/vikunja": {{
					Name: ".sshkey/../evil", Hosts: []string{"host.example.com"},
					PrivateKey: "priv\n",
				}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{EnabledBundles: []string{"vikunja"}}); err != nil {
		t.Fatal(err)
	}

	_, err := pullEnabledSecretsBundles(context.Background(), projectRoot, []string{"vikunja"}, nil)
	if err == nil {
		t.Fatal("非法 SSH Key 名应导致 pull 失败")
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "bundles", "vikunja", ".env", "vikunja.env")); !os.IsNotExist(err) {
		t.Fatal("SSH 校验失败时不应写入 Secure Note")
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".ssh")); len(entries) != 0 {
		t.Fatalf("SSH 校验失败时不应写入 ~/.ssh: %v", entries)
	}
}

// 平面隔离（ADR 0009）：project 上下文的 plan 只含项目平面 target，不含机器平面。
func TestPlanSecretsSync_ProjectPlaneOnly(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	if err := config.SaveGlobalConfig(&types.GlobalConfig{EnabledBundles: []string{"woa", "vikunja"}}); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "demo",
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}

	plan, err := planSecretsSync(projectRoot, []string{"vikunja"}, &secrets.Config{})
	if err != nil {
		t.Fatalf("planSecretsSync() = %v", err)
	}
	var projectBundle int
	for _, target := range plan.Targets {
		if secrets.IsMachinePlane(target.Plane) {
			t.Fatalf("project 上下文不应产生机器平面 target: %+v", target)
		}
		if target.Kind == secrets.SyncKindBundle && target.Name == "vikunja" {
			projectBundle++
		}
	}
	if projectBundle != 1 {
		t.Fatalf("projectBundle = %d, targets=%+v", projectBundle, plan.Targets)
	}
}

func TestPlanWorkspaceSecretsSync_PUsesHomeOnlyAndFixedPlane(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"my-app/dec.yaml":                        "name: my-app\nrequires: [shared]\n",
		"shared/dec.yaml":                        "name: shared\n",
		"shared/public/project/rules/shared.mdc": "shared",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "my-app"}); err != nil {
		t.Fatal(err)
	}

	projectPlan, err := planWorkspaceSecretsSync(
		NewWorkspace(WorkspaceProject, projectRoot),
		[]string{"my-app", "shared"},
		&secrets.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(projectPlan.Targets) != 1 {
		t.Fatalf("requires/private 不应加入 project secrets plan: %#v", projectPlan.Targets)
	}
	target := projectPlan.Targets[0]
	if target.Kind != secrets.SyncKindP || target.Name != "my-app" ||
		target.Folder != "my-app/private/project" || target.LocalRoot != ".secrets/my-app" {
		t.Fatalf("project P target = %#v", target)
	}

	userPlan, err := planWorkspaceSecretsSync(
		NewWorkspace(WorkspaceUser, ""),
		[]string{"shared"},
		&secrets.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(userPlan.Targets) != 1 || userPlan.Targets[0].Folder != "shared/private/user" ||
		userPlan.Targets[0].LocalRoot != "shared" {
		t.Fatalf("user P target = %#v", userPlan.Targets)
	}
}

func TestValidateNoPPrivateGitOverlapRejectsSameLogicalPath(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"my-app/dec.yaml":                          "name: my-app\n",
		"my-app/private/project/rules/private.mdc": "non-secret",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	target, err := secrets.NewPSyncTarget("my-app", secrets.SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	err = validateNoPPrivateGitOverlap(
		[]secrets.SyncTarget{target},
		[][]secrets.SecureNote{{{RelativePath: "rules/private.mdc", Content: "secret"}}},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "同时由 Git 与 Bitwarden") {
		t.Fatalf("同一 P/plane/相对路径应冲突，got %v", err)
	}
}

func TestAddGCMConflictRejectsSameProjectCredentialScope(t *testing.T) {
	seen := map[string]string{}
	first := secrets.SecureNote{
		RelativePath: ".gcm/first.yaml",
		Content:      "host: git.example.com\nusername: first\npassword: one\npath: team/repo\n",
	}
	second := secrets.SecureNote{
		RelativePath: ".gcm/second.yaml",
		Content:      "host: git.example.com\nusername: second\npassword: two\npath: team/repo\n",
	}
	if err := addGCMConflict(seen, "P one", first); err != nil {
		t.Fatal(err)
	}
	err := addGCMConflict(seen, "P two", second)
	if err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("同 protocol/host/path 的 GCM 应硬冲突: %v", err)
	}
}

func TestPullPPrivateProjectCredentialsAreWorkspaceScoped(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	home := useTempHomeForSSH(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"my-app/dec.yaml": "name: my-app\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	projectRoot := t.TempDir()
	if out, err := exec.Command("git", "-C", projectRoot, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", projectRoot, "remote", "add", "origin", "https://git.example.com/team/repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}
	if err := config.NewProjectConfigManager(projectRoot).SaveProjectConfig(&types.ProjectConfig{ProjectName: "my-app"}); err != nil {
		t.Fatal(err)
	}

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"my-app/private/project": {{
					RelativePath: ".gcm/repo.yaml",
					Content:      "host: git.example.com\nusername: bot\npassword: token\n",
				}},
			},
			SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
				"my-app/private/project": {{
					Name: ".sshkey/deploy", Hosts: []string{"ssh.example.com"},
					PrivateKey: "private\n", PublicKey: "public\n",
				}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	var gcmCalls [][]string
	reg := handler.NewRegistry()
	gcm := handler.NewGCMHandler(func(_ context.Context, _ string, args ...string) error {
		gcmCalls = append(gcmCalls, append([]string(nil), args...))
		return nil
	})
	reg.Register(gcm)
	restore := handler.SetDefault(reg)
	t.Cleanup(restore)

	summary, err := pullEnabledSecretsBundles(context.Background(), projectRoot, []string{"my-app"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SSHKeyCount != 1 || summary.HandlerCount != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if len(gcmCalls) != 1 || strings.Join(gcmCalls[0], " ") != "-C "+projectRoot+" credential approve" {
		t.Fatalf("GCM 未按工作区 Apply: %#v", gcmCalls)
	}
	if ok, err := secrets.InspectProjectSSHKeyLanding(projectRoot, "my-app", "deploy"); err != nil || !ok {
		t.Fatalf("project SSH Inspect = %v, %v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "dec_my-app_deploy")); !os.IsNotExist(err) {
		t.Fatal("private/project 不得复用 user 平面 SSH 文件名")
	}
	paths, err := secrets.ProjectCredentialScopePaths(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	sshRaw, err := os.ReadFile(paths.SSHFragment)
	if err != nil || !strings.Contains(string(sshRaw), "Host ssh.example.com") {
		t.Fatalf("project SSH fragment: err=%v content=%s", err, sshRaw)
	}
	if raw, readErr := os.ReadFile(filepath.Join(home, ".ssh", "config.d", "dec.conf")); readErr == nil &&
		strings.Contains(string(raw), "ssh.example.com") {
		t.Fatalf("project Host 污染了 user config: %s", raw)
	}
}
