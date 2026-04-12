package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureMiseLocalTomlFile_CreatesDefaultFile(t *testing.T) {
	projectRoot := t.TempDir()

	created, err := ensureMiseLocalTomlFile(projectRoot)
	if err != nil {
		t.Fatalf("ensureMiseLocalTomlFile() 失败: %v", err)
	}
	if !created {
		t.Fatal("首次调用应创建 mise.local.toml")
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "mise.local.toml"))
	if err != nil {
		t.Fatalf("读取 mise.local.toml 失败: %v", err)
	}
	if string(data) != defaultMiseLocalTomlContent {
		t.Fatalf("mise.local.toml 内容不匹配, got %q", string(data))
	}
}

func TestEnsureMiseLocalTomlFile_PreservesExistingFile(t *testing.T) {
	projectRoot := t.TempDir()
	existing := "[env]\nKEEP = \"1\"\n"
	if err := os.WriteFile(filepath.Join(projectRoot, "mise.local.toml"), []byte(existing), 0644); err != nil {
		t.Fatalf("预写 mise.local.toml 失败: %v", err)
	}

	created, err := ensureMiseLocalTomlFile(projectRoot)
	if err != nil {
		t.Fatalf("ensureMiseLocalTomlFile() 失败: %v", err)
	}
	if created {
		t.Fatal("已有文件时不应重复创建")
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "mise.local.toml"))
	if err != nil {
		t.Fatalf("读取 mise.local.toml 失败: %v", err)
	}
	if string(data) != existing {
		t.Fatalf("不应覆盖已有 mise.local.toml, got %q", string(data))
	}
}

func TestEnsureMiseLocalTomlGitignore_AppendsEntry(t *testing.T) {
	projectRoot := t.TempDir()
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(".env\n"), 0644); err != nil {
		t.Fatalf("写入 .gitignore 失败: %v", err)
	}

	updated, err := ensureMiseLocalTomlGitignore(projectRoot)
	if err != nil {
		t.Fatalf("ensureMiseLocalTomlGitignore() 失败: %v", err)
	}
	if !updated {
		t.Fatal("缺少条目时应更新 .gitignore")
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("读取 .gitignore 失败: %v", err)
	}
	if string(data) != ".env\nmise.local.toml\n" {
		t.Fatalf(".gitignore 内容不匹配, got %q", string(data))
	}
}

func TestEnsureMiseLocalTomlGitignore_CreatesFileWhenMissing(t *testing.T) {
	projectRoot := t.TempDir()

	updated, err := ensureMiseLocalTomlGitignore(projectRoot)
	if err != nil {
		t.Fatalf("ensureMiseLocalTomlGitignore() 失败: %v", err)
	}
	if !updated {
		t.Fatal("缺少 .gitignore 时应创建")
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, ".gitignore"))
	if err != nil {
		t.Fatalf("读取 .gitignore 失败: %v", err)
	}
	if string(data) != "mise.local.toml\n" {
		t.Fatalf(".gitignore 内容不匹配, got %q", string(data))
	}
}

func TestEnsureMiseLocalTomlGitignore_IsIdempotent(t *testing.T) {
	projectRoot := t.TempDir()
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	original := ".env\nmise.local.toml\n"
	if err := os.WriteFile(gitignorePath, []byte(original), 0644); err != nil {
		t.Fatalf("写入 .gitignore 失败: %v", err)
	}

	updated, err := ensureMiseLocalTomlGitignore(projectRoot)
	if err != nil {
		t.Fatalf("ensureMiseLocalTomlGitignore() 失败: %v", err)
	}
	if updated {
		t.Fatal("已有条目时不应重复更新")
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("读取 .gitignore 失败: %v", err)
	}
	if string(data) != original {
		t.Fatalf("已有条目时不应修改 .gitignore, got %q", string(data))
	}
}

func TestGitignoreHasMiseLocalTomlEntry(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "plain entry", content: ".env\nmise.local.toml\n", want: true},
		{name: "root entry", content: ".env\n/mise.local.toml\n", want: true},
		{name: "comment only", content: "# mise.local.toml\n", want: false},
		{name: "different file", content: "mise.toml\n", want: false},
	}

	for _, tt := range tests {
		if got := gitignoreHasMiseLocalTomlEntry(tt.content); got != tt.want {
			t.Fatalf("%s: gitignoreHasMiseLocalTomlEntry() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestTrustMiseLocalToml_RunsMiseTrust(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "mise.local.toml"), []byte(defaultMiseLocalTomlContent), 0644); err != nil {
		t.Fatalf("写入 mise.local.toml 失败: %v", err)
	}

	oldExecCommand := execCommand
	execCommand = fakeMiseTrustCommand(t, projectRoot, false, "")
	defer func() { execCommand = oldExecCommand }()

	if err := trustMiseLocalToml(projectRoot); err != nil {
		t.Fatalf("trustMiseLocalToml() 失败: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "mise-invocation.txt"))
	if err != nil {
		t.Fatalf("读取 mise 调用记录失败: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "trust|mise.local.toml" {
		t.Fatalf("mise 调用参数不匹配, got %q", got)
	}
}

func TestTrustMiseLocalToml_ReportsCommandFailure(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "mise.local.toml"), []byte(defaultMiseLocalTomlContent), 0644); err != nil {
		t.Fatalf("写入 mise.local.toml 失败: %v", err)
	}

	oldExecCommand := execCommand
	execCommand = fakeMiseTrustCommand(t, projectRoot, true, "mock mise failure")
	defer func() { execCommand = oldExecCommand }()

	err := trustMiseLocalToml(projectRoot)
	if err == nil {
		t.Fatal("mise trust 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "mock mise failure") {
		t.Fatalf("错误信息应包含命令输出, got %v", err)
	}
}

func fakeMiseTrustCommand(t *testing.T, projectRoot string, fail bool, message string) func(string, ...string) *exec.Cmd {
	t.Helper()

	return func(name string, args ...string) *exec.Cmd {
		allArgs := append([]string{"-test.run=TestHelperProcessMiseTrust", "--", projectRoot}, args...)
		cmd := exec.Command(os.Args[0], allArgs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
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

func TestHelperProcessMiseTrust(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	projectRoot := os.Getenv("DEC_HELPER_PROJECT_ROOT")
	if len(os.Args) < 5 {
		os.Exit(2)
	}
	args := os.Args[4:]
	record := strings.Join(args, "|") + "\n"
	if err := os.WriteFile(filepath.Join(projectRoot, "mise-invocation.txt"), []byte(record), 0644); err != nil {
		os.Exit(3)
	}
	if os.Getenv("DEC_HELPER_FAIL") == "1" {
		_, _ = os.Stderr.WriteString(os.Getenv("DEC_HELPER_MESSAGE"))
		os.Exit(1)
	}
	os.Exit(0)
}
