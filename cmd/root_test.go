package cmd

import (
	"io"
	"os"
	"strings"
	"testing"
)

func stubEntryExecutionForRootTest(t *testing.T) {
	t.Helper()

	oldRunCLIMode := runCLIMode
	t.Cleanup(func() {
		runCLIMode = oldRunCLIMode
	})
}

func TestRootVersionString(t *testing.T) {
	oldVersion := appVersion
	oldBuildTime := appBuildTime
	defer func() {
		appVersion = oldVersion
		appBuildTime = oldBuildTime
		RootCmd.Version = getVersionString()
	}()

	SetVersion("v1.10.40", "2026-04-03_00:00:00")
	if got := GetVersion(); got != "v1.10.40" {
		t.Fatalf("GetVersion() = %q, 期望 %q", got, "v1.10.40")
	}
	if RootCmd.Version == "" {
		t.Fatal("RootCmd.Version 不应为空")
	}
}

func TestGetVersionIgnoresWorkingDirVersionFile(t *testing.T) {
	tempDir := t.TempDir()
	versionFile := tempDir + "/version.json"
	if err := os.WriteFile(versionFile, []byte("{\n  \"version\": \"v9.9.9\"\n}\n"), 0644); err != nil {
		t.Fatalf("写入 version.json 失败: %v", err)
	}

	oldVersion := appVersion
	oldBuildTime := appBuildTime
	defer func() {
		appVersion = oldVersion
		appBuildTime = oldBuildTime
		RootCmd.Version = getVersionString()
	}()

	appVersion = "dev"
	appBuildTime = "unknown"
	RootCmd.Version = getVersionString()
	chdirForTest(t, tempDir)

	if got := GetVersion(); got != "dev" {
		t.Fatalf("GetVersion() = %q, 期望 %q（工作目录 version.json 不得参与版本判定）", got, "dev")
	}
	if RootCmd.Version != "dev" {
		t.Fatalf("RootCmd.Version = %q, 期望 %q", RootCmd.Version, "dev")
	}
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd 失败: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldDir)
	})
}

func TestExecuteAlwaysUsesCLI(t *testing.T) {
	stubEntryExecutionForRootTest(t)

	cliCalled := false
	runCLIMode = func(args []string, stdout, stderr io.Writer) error {
		cliCalled = true
		return nil
	}

	if err := Execute(nil, os.Stdin, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("Execute() 返回错误: %v", err)
	}
	if !cliCalled {
		t.Fatal("无参应走 CLI（提示使用 Console），不再启动 TUI")
	}
}

func TestExecuteRoutesInternalFreshnessCheckToCLI(t *testing.T) {
	stubEntryExecutionForRootTest(t)

	var gotArgs []string
	runCLIMode = func(args []string, stdout, stderr io.Writer) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}

	if err := Execute([]string{"__freshness-check", "--project-root", "/tmp/proj"}, os.Stdin, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("Execute() 返回错误: %v", err)
	}
	if len(gotArgs) < 1 || gotArgs[0] != "__freshness-check" {
		t.Fatalf("CLI args = %#v, 期望 freshness 内部命令", gotArgs)
	}
}

func TestExecuteRoutesHelpToCLI(t *testing.T) {
	stubEntryExecutionForRootTest(t)

	var gotArgs []string
	runCLIMode = func(args []string, stdout, stderr io.Writer) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}

	if err := Execute([]string{"--help"}, os.Stdin, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("Execute() 返回错误: %v", err)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "--help" {
		t.Fatalf("CLI args = %#v, 期望 %#v", gotArgs, []string{"--help"})
	}
}

func TestNoArgsRunEPointsToConsole(t *testing.T) {
	err := RootCmd.RunE(RootCmd, nil)
	if err == nil {
		t.Fatal("无参应返回错误，提示使用 Console")
	}
	if !strings.Contains(err.Error(), "桌面管理客户端") {
		t.Fatalf("错误信息应指向 Console，得到: %v", err)
	}
}

func TestIsInternalCLIArgs(t *testing.T) {
	if !isInternalCLIArgs([]string{"__freshness-check", "--project-root", "/x"}) {
		t.Fatal("freshness 内部命令应识别为 CLI 参数")
	}
	if !isInternalCLIArgs([]string{"__service-setup"}) {
		t.Fatal("service-setup 内部命令应识别为 CLI 参数")
	}
	if isInternalCLIArgs([]string{"mcp", "--project-root", "/x"}) {
		t.Fatal("mcp 已拆为 dec-mcp，不应再走 dec 内部 CLI")
	}
	if isInternalCLIArgs([]string{"pull"}) {
		t.Fatal("已移除的用户子命令不应走内部 CLI 短路")
	}
}

func TestMCPSubcommandRemoved(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"mcp"})
	if err == nil && cmd != nil && cmd.Name() == "mcp" {
		t.Fatal("mcp 已拆为 dec-mcp，不应注册在 dec 根命令")
	}
}

func TestRemovedSubcommandReturnsError(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"pull"})
	if err == nil && cmd != nil && cmd.Name() == "pull" {
		t.Fatal("pull 子命令应已移除")
	}
}

func TestUpdateSubcommandRemoved(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"update"})
	if err == nil && cmd != nil && cmd.Name() == "update" {
		t.Fatal("update 子命令应已移除；自更新走 Console")
	}
}

func TestGlobalAndUserFlagsRemoved(t *testing.T) {
	if RootCmd.PersistentFlags().Lookup("global") != nil {
		t.Fatal("--global 已随 TUI 移除")
	}
	if RootCmd.PersistentFlags().Lookup("user") != nil {
		t.Fatal("--user 已随 TUI 移除")
	}
}
