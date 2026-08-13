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

	_, err = PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err == nil {
		t.Fatal("期望 secrets 路径重叠时 pull 失败")
	}
	if !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("错误应描述路径冲突: %v", err)
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
	// 仅本机启用，项目侧留空：同时覆盖 ADR 0003 的用户级 pull 路径。
	if err := secrets.SaveConfig(&secrets.Config{UserEnabledBundles: []string{"cli"}}); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{IDEs: []string{"cursor"}}); err != nil {
		t.Fatal(err)
	}

	// 无 session：secrets 阶段会尝试 web unlock，测试环境下被 guard 拒绝。
	secrets.ClearSession()

	if _, err := PullProjectAssets(context.Background(), projectRoot, "", nil); err == nil {
		t.Fatal("期望 Bitwarden 未解锁时 pull 返回错误")
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

// pull 不再做「停用即清理」：密文落在 .secrets/ 同步根，没有可以安全 RemoveAll 的目录。
// 已存在的项目内文件必须原样留着，等 Remote 页逐条确认。
func TestPullEnabledSecretsBundles_NeverDeletesExistingProjectFiles(t *testing.T) {
	setupSecretsConfigForPushTest(t)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return &secrets.StubClient{} }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "Demo"}); err != nil {
		t.Fatal(err)
	}
	untouched := filepath.Join(projectRoot, ".secrets", "project", "config", "server.yaml")
	if err := os.MkdirAll(filepath.Dir(untouched), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(untouched, []byte("keep: true\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := pullEnabledSecretsBundles(context.Background(), projectRoot, nil, nil); err != nil {
		t.Fatalf("pullEnabledSecretsBundles() 失败: %v", err)
	}
	if _, err := os.Stat(untouched); err != nil {
		t.Fatalf("项目内既有文件不应被 pull 清理: %v", err)
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
	if !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("错误应描述冲突: %v", err)
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
				"bundle/vikunja": {{RelativePath: "env/vikunja.env", Content: "VIKUNJA_API_TOKEN=abc\n"}},
			},
			SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
				"bundle/vikunja": {{
					Name: "deploy", Hosts: []string{"vikunja.example.com"},
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
	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "bundles", "vikunja", "env", "vikunja.env")); err != nil {
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
				"bundle/vikunja": {{RelativePath: "env/vikunja.env", Content: "TOKEN=1\n"}},
			},
			SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
				"bundle/vikunja": {{
					Name: "../evil", Hosts: []string{"host.example.com"},
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
	if _, err := os.Stat(filepath.Join(projectRoot, ".secrets", "bundles", "vikunja", "env", "vikunja.env")); !os.IsNotExist(err) {
		t.Fatal("SSH 校验失败时不应写入 Secure Note")
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".ssh")); len(entries) != 0 {
		t.Fatalf("SSH 校验失败时不应写入 ~/.ssh: %v", entries)
	}
}

func TestPlanSecretsSync_MergesUserEnabledBundles(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "demo",
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}

	plan, err := planSecretsSync(projectRoot, []string{"vikunja"}, &secrets.Config{
		UserEnabledBundles: []string{"woa", "vikunja"},
	})
	if err != nil {
		t.Fatalf("planSecretsSync() = %v", err)
	}
	names := make([]string, 0, len(plan.Targets))
	for _, target := range plan.Targets {
		if target.Kind == secrets.SyncKindBundle {
			names = append(names, target.Name)
		}
	}
	if len(names) != 2 || names[0] != "vikunja" || names[1] != "woa" {
		t.Fatalf("bundle targets = %#v, 期望 [vikunja woa]", names)
	}
}
