package cmd

import (
	"io"
	"os"
	"testing"

	"github.com/shichao402/Dec/internal/app"
)

func setEnvForRootTest(t *testing.T, key, value string) {
	t.Helper()
	oldValue, existed := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("设置环境变量失败: %v", err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, oldValue)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func stubEntryExecutionForRootTest(t *testing.T) {
	t.Helper()

	oldDetectTTY := detectTTY
	oldGetWorkingDir := getWorkingDir
	oldRunCLIMode := runCLIMode
	oldRunTUIMode := runTUIMode
	oldRunTUIWorkspaceMode := runTUIWorkspaceMode
	oldEmitUpdateHint := emitUpdateHint

	t.Cleanup(func() {
		detectTTY = oldDetectTTY
		getWorkingDir = oldGetWorkingDir
		runCLIMode = oldRunCLIMode
		runTUIMode = oldRunTUIMode
		runTUIWorkspaceMode = oldRunTUIWorkspaceMode
		emitUpdateHint = oldEmitUpdateHint
	})

	emitUpdateHint = func(io.Writer) {}
}

func TestExecuteRoutesUserFlagToUserTUI(t *testing.T) {
	stubEntryExecutionForRootTest(t)
	setEnvForRootTest(t, "TERM", "xterm-256color")
	setEnvForRootTest(t, "DEC_NO_TUI", "")
	detectTTY = func(*os.File) bool { return true }
	getWorkingDir = func() (string, error) { return t.TempDir(), nil }
	runTUIMode = func(string, io.Reader, io.Writer) error {
		t.Fatal("--user 不应启动项目平面 TUI")
		return nil
	}
	var gotPlane app.WorkspacePlane
	runTUIWorkspaceMode = func(workspace app.Workspace, _ io.Reader, _ io.Writer) error {
		gotPlane = workspace.EffectivePlane()
		return nil
	}

	if err := Execute([]string{"--user"}, os.Stdin, os.Stdout, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if gotPlane != app.WorkspaceUser {
		t.Fatalf("workspace plane = %q", gotPlane)
	}
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

func TestExecuteRoutesInteractiveNoArgsToTUI(t *testing.T) {
	stubEntryExecutionForRootTest(t)
	setEnvForRootTest(t, "TERM", "xterm-256color")
	setEnvForRootTest(t, "DEC_NO_TUI", "")

	projectRoot := t.TempDir()
	detectTTY = func(*os.File) bool { return true }
	getWorkingDir = func() (string, error) { return projectRoot, nil }

	cliCalled := false
	runCLIMode = func(args []string, stdout, stderr io.Writer) error {
		cliCalled = true
		return nil
	}

	var gotProjectRoot string
	runTUIMode = func(projectRoot string, input io.Reader, output io.Writer) error {
		gotProjectRoot = projectRoot
		return nil
	}

	if err := Execute(nil, os.Stdin, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("Execute() 返回错误: %v", err)
	}
	if cliCalled {
		t.Fatal("无参交互式终端应进入 TUI，而不是 CLI")
	}
	if gotProjectRoot != projectRoot {
		t.Fatalf("TUI projectRoot = %q, 期望 %q", gotProjectRoot, projectRoot)
	}
}

func TestExecuteRoutesToCLIWhenSubcommandRequested(t *testing.T) {
	stubEntryExecutionForRootTest(t)
	setEnvForRootTest(t, "TERM", "xterm-256color")
	detectTTY = func(*os.File) bool { return true }

	var gotArgs []string
	runCLIMode = func(args []string, stdout, stderr io.Writer) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}
	runTUIMode = func(projectRoot string, input io.Reader, output io.Writer) error {
		t.Fatal("显式参数应走 CLI")
		return nil
	}

	if err := Execute([]string{"pull"}, os.Stdin, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("Execute() 返回错误: %v", err)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "pull" {
		t.Fatalf("CLI args = %#v, 期望 %#v", gotArgs, []string{"pull"})
	}
}

func TestExecuteRoutesInternalFreshnessCheckToCLI(t *testing.T) {
	stubEntryExecutionForRootTest(t)
	setEnvForRootTest(t, "TERM", "xterm-256color")
	detectTTY = func(*os.File) bool { return true }

	var gotArgs []string
	runCLIMode = func(args []string, stdout, stderr io.Writer) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}
	runTUIMode = func(projectRoot string, input io.Reader, output io.Writer) error {
		t.Fatal("内部 freshness 命令应走 CLI")
		return nil
	}

	if err := Execute([]string{"__freshness-check", "--project-root", "/tmp/proj"}, os.Stdin, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("Execute() 返回错误: %v", err)
	}
	if len(gotArgs) < 1 || gotArgs[0] != "__freshness-check" {
		t.Fatalf("CLI args = %#v, 期望 freshness 内部命令", gotArgs)
	}
}

func TestExecuteRoutesToCLIWhenDisabledByEnv(t *testing.T) {
	stubEntryExecutionForRootTest(t)
	setEnvForRootTest(t, "TERM", "xterm-256color")
	setEnvForRootTest(t, "DEC_NO_TUI", "1")
	detectTTY = func(*os.File) bool { return true }

	cliCalled := false
	runCLIMode = func(args []string, stdout, stderr io.Writer) error {
		cliCalled = true
		return nil
	}
	runTUIMode = func(projectRoot string, input io.Reader, output io.Writer) error {
		t.Fatal("DEC_NO_TUI=1 时不应进入 TUI")
		return nil
	}

	if err := Execute(nil, os.Stdin, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("Execute() 返回错误: %v", err)
	}
	if !cliCalled {
		t.Fatal("DEC_NO_TUI=1 时应回退到 CLI")
	}
}

func TestExecuteRoutesToCLIWhenStdoutIsNotTTY(t *testing.T) {
	stubEntryExecutionForRootTest(t)
	setEnvForRootTest(t, "TERM", "xterm-256color")

	detectTTY = func(file *os.File) bool {
		return file != os.Stdout
	}

	cliCalled := false
	runCLIMode = func(args []string, stdout, stderr io.Writer) error {
		cliCalled = true
		return nil
	}
	runTUIMode = func(projectRoot string, input io.Reader, output io.Writer) error {
		t.Fatal("非 TTY 输出时不应进入 TUI")
		return nil
	}

	if err := Execute(nil, os.Stdin, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("Execute() 返回错误: %v", err)
	}
	if !cliCalled {
		t.Fatal("非 TTY 输出时应回退到 CLI")
	}
}

func TestExecuteRoutesToCLIWhenHelpRequested(t *testing.T) {
	stubEntryExecutionForRootTest(t)
	setEnvForRootTest(t, "TERM", "xterm-256color")
	detectTTY = func(*os.File) bool { return true }

	var gotArgs []string
	runCLIMode = func(args []string, stdout, stderr io.Writer) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}
	runTUIMode = func(projectRoot string, input io.Reader, output io.Writer) error {
		t.Fatal("--help 应走 CLI")
		return nil
	}

	if err := Execute([]string{"--help"}, os.Stdin, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("Execute() 返回错误: %v", err)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "--help" {
		t.Fatalf("CLI args = %#v, 期望 %#v", gotArgs, []string{"--help"})
	}
}

func TestExecuteRoutesToCLIWhenTermIsDumb(t *testing.T) {
	stubEntryExecutionForRootTest(t)
	setEnvForRootTest(t, "TERM", "dumb")
	setEnvForRootTest(t, "DEC_NO_TUI", "")
	detectTTY = func(*os.File) bool { return true }

	cliCalled := false
	runCLIMode = func(args []string, stdout, stderr io.Writer) error {
		cliCalled = true
		return nil
	}
	runTUIMode = func(projectRoot string, input io.Reader, output io.Writer) error {
		t.Fatal("TERM=dumb 时不应进入 TUI")
		return nil
	}

	if err := Execute(nil, os.Stdin, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("Execute() 返回错误: %v", err)
	}
	if !cliCalled {
		t.Fatal("TERM=dumb 时应回退到 CLI")
	}
}

func TestIsInternalCLIArgs(t *testing.T) {
	if !isInternalCLIArgs([]string{"__freshness-check", "--project-root", "/x"}) {
		t.Fatal("freshness 内部命令应识别为 CLI 参数")
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

func TestUpdateSubcommandRegistered(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"update"})
	if err != nil {
		t.Fatalf("查找 update 子命令失败: %v", err)
	}
	if cmd == nil || cmd.Name() != "update" {
		t.Fatal("update 子命令应已注册")
	}
}
