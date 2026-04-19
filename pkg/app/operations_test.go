package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

func TestPullProjectAssetsSkipsWithoutEnabledAssets(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	result, err := PullProjectAssets(t.TempDir(), "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.SkippedReason != "config.yaml 中没有已启用的资产" {
		t.Fatalf("SkippedReason = %q, 期望 %q", result.SkippedReason, "config.yaml 中没有已启用的资产")
	}
}

func TestPullProjectAssetsSkipsWhenEnabledAssetsDoNotExistInAvailable(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		Available: &types.AssetList{
			Skills: []types.AssetRef{{Name: "another-workflow", Vault: "default"}},
		},
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := PullProjectAssets(projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.SkippedReason != "没有有效的已启用资产可拉取" {
		t.Fatalf("SkippedReason = %q, 期望 %q", result.SkippedReason, "没有有效的已启用资产可拉取")
	}
	if len(result.ValidationWarnings) != 1 || !strings.Contains(result.ValidationWarnings[0], "project-workflow") {
		t.Fatalf("ValidationWarnings = %#v, 期望包含 project-workflow", result.ValidationWarnings)
	}
}

func TestPullProjectAssetsInstallsAssetsAndReportsProgress(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"default/skills/project-workflow/SKILL.md": `---
name: project-workflow
---
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs: []string{"cursor"},
		Available: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	oldExec := operationsExecCommand
	operationsExecCommand = fakeMiseTrustCommandProjectAppTest(t, projectRoot, false, "")
	defer func() { operationsExecCommand = oldExec }()

	var events []OperationEvent
	result, err := PullProjectAssets(projectRoot, "", ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.RequestedCount != 1 || result.PulledCount != 1 || result.FailedCount != 0 {
		t.Fatalf("结果计数异常: %+v", result)
	}
	if !result.MiseLocalCreated || !result.GitignoreUpdated || !result.MiseTrustSucceeded {
		t.Fatalf("mise 收尾状态异常: %+v", result)
	}
	if len(result.EffectiveIDEs) != 1 || result.EffectiveIDEs[0] != "cursor" {
		t.Fatalf("EffectiveIDEs = %#v, 期望 %#v", result.EffectiveIDEs, []string{"cursor"})
	}
	if strings.TrimSpace(result.VersionCommit) == "" {
		t.Fatal("VersionCommit 不应为空")
	}

	if _, err := os.Stat(filepath.Join(projectRoot, ".dec", "cache", "default", "skills", "project-workflow", "SKILL.md")); err != nil {
		t.Fatalf("缓存文件应存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-project-workflow", "SKILL.md")); err != nil {
		t.Fatalf("安装后的 skill 应存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".dec", ".version")); err != nil {
		t.Fatalf(".dec/.version 应存在: %v", err)
	}

	invocation, err := os.ReadFile(filepath.Join(projectRoot, "mise-invocation.txt"))
	if err != nil {
		t.Fatalf("读取 mise 调用记录失败: %v", err)
	}
	if got := strings.TrimSpace(string(invocation)); got != "trust|mise.local.toml" {
		t.Fatalf("mise 调用参数 = %q, 期望 %q", got, "trust|mise.local.toml")
	}

	var sawStart, sawFinish bool
	for _, event := range events {
		if event.Scope == "pull.start" {
			sawStart = true
		}
		if event.Scope == "pull.finish" {
			sawFinish = true
		}
	}
	if !sawStart || !sawFinish {
		t.Fatalf("事件流缺少开始或结束事件: %#v", events)
	}
}

func TestPullProjectAssetsContinuesWhenMiseTrustFails(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"default/skills/project-workflow/SKILL.md": `---
name: project-workflow
---
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs: []string{"cursor"},
		Available: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	oldExec := operationsExecCommand
	operationsExecCommand = fakeMiseTrustCommandProjectAppTest(t, projectRoot, true, "mock trust failed")
	defer func() { operationsExecCommand = oldExec }()

	result, err := PullProjectAssets(projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.PulledCount != 1 || result.FailedCount != 0 {
		t.Fatalf("拉取结果异常: %+v", result)
	}
	if result.MiseTrustSucceeded {
		t.Fatalf("mise trust 失败时不应标记成功: %+v", result)
	}
	if len(result.NonFatalWarnings) != 1 || !strings.Contains(result.NonFatalWarnings[0], "mock trust failed") {
		t.Fatalf("NonFatalWarnings = %#v, 期望包含 mock trust failed", result.NonFatalWarnings)
	}
}

func fakeMiseTrustCommandProjectAppTest(t *testing.T, projectRoot string, fail bool, message string) func(string, ...string) *exec.Cmd {
	t.Helper()

	return func(name string, args ...string) *exec.Cmd {
		allArgs := append([]string{"-test.run=TestHelperProcessMiseTrustProjectApp", "--", projectRoot}, args...)
		cmd := exec.Command(os.Args[0], allArgs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS_APP=1",
			"DEC_HELPER_PROJECT_ROOT="+projectRoot,
		)
		if fail {
			cmd.Env = append(cmd.Env,
				"DEC_HELPER_FAIL=1",
				"DEC_HELPER_MESSAGE="+message,
			)
		}
		return cmd
	}
}

func TestHelperProcessMiseTrustProjectApp(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS_APP") != "1" {
		return
	}

	projectRoot := os.Getenv("DEC_HELPER_PROJECT_ROOT")
	if len(os.Args) < 5 {
		os.Exit(2)
	}
	args := os.Args[4:]
	record := strings.Join(args, "|") + string([]byte{'\n'})
	if err := os.WriteFile(filepath.Join(projectRoot, "mise-invocation.txt"), []byte(record), 0644); err != nil {
		os.Exit(3)
	}
	if os.Getenv("DEC_HELPER_FAIL") == "1" {
		_, _ = os.Stderr.WriteString(os.Getenv("DEC_HELPER_MESSAGE"))
		os.Exit(1)
	}
	os.Exit(0)
}
