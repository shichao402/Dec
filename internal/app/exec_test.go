package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/secrets"
)

func TestWrapMCPServerWithExec_WrapsCommandAndStripsPlaceholders(t *testing.T) {
	cmd, args, env := WrapMCPServerWithExec(
		`D:\proj`,
		"vikunja",
		"dec",
		"npx",
		[]string{"-y", "@shichao402/vikunja-mcp"},
		map[string]string{
			"VIKUNJA_URL":        "${VIKUNJA_URL}",
			"PKV_WORKSPACE_ROOT": "D:/workspace",
		},
	)
	if cmd != "dec" {
		t.Fatalf("command = %q", cmd)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "exec") || !strings.Contains(joined, "--bundle") || !strings.Contains(joined, "vikunja") {
		t.Fatalf("args = %#v", args)
	}
	if !strings.Contains(joined, "--") || args[len(args)-3] != "npx" {
		t.Fatalf("应保留真实命令在 -- 之后: %#v", args)
	}
	if _, ok := env["VIKUNJA_URL"]; ok {
		t.Fatalf("应去掉 ${VAR} 占位 env: %#v", env)
	}
	if env["PKV_WORKSPACE_ROOT"] != "D:/workspace" {
		t.Fatalf("应保留非占位 env: %#v", env)
	}
}

func TestBuildExecEnviron_LoadsBundleAndProjectEnv(t *testing.T) {
	root := t.TempDir()
	bundleTarget, err := secrets.NewBundleSyncTarget("vikunja", "")
	if err != nil {
		t.Fatal(err)
	}
	projectTarget, err := secrets.NewProjectSyncTarget("Demo", "")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.ToSlash(filepath.Join(bundleTarget.LocalRoot, "env", "vikunja.env")), "TOKEN=from-bundle\nSHARED=bundle\n")
	mustWrite(filepath.ToSlash(filepath.Join(projectTarget.LocalRoot, "env", "app.env")), "SHARED=project\nPROJECT_ONLY=1\n")

	env, err := BuildExecEnviron(root, "vikunja", []string{"PATH=/bin", "SHARED=base"})
	if err != nil {
		t.Fatalf("BuildExecEnviron() = %v", err)
	}
	got := map[string]string{}
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq > 0 {
			got[kv[:eq]] = kv[eq+1:]
		}
	}
	if got["TOKEN"] != "from-bundle" {
		t.Fatalf("TOKEN = %q", got["TOKEN"])
	}
	if got["PROJECT_ONLY"] != "1" {
		t.Fatalf("PROJECT_ONLY = %q", got["PROJECT_ONLY"])
	}
	// project 覆盖 bundle
	if got["SHARED"] != "project" {
		t.Fatalf("SHARED = %q, 期望 project 覆盖", got["SHARED"])
	}
	if got["PATH"] != "/bin" {
		t.Fatalf("PATH 应保留基环境: %q", got["PATH"])
	}
}

func TestRunExecWithSecrets_InjectsEnvIntoChild(t *testing.T) {
	root := t.TempDir()
	bundleTarget, err := secrets.NewBundleSyncTarget("vikunja", "")
	if err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(root, filepath.FromSlash(bundleTarget.LocalRoot), "env", "vikunja.env")
	if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("DEC_EXEC_PROBE=yes\n"), 0600); err != nil {
		t.Fatal(err)
	}

	var command []string
	if runtime.GOOS == "windows" {
		command = []string{"cmd", "/C", "if not defined DEC_EXEC_PROBE (exit 2) else (exit 0)"}
	} else {
		command = []string{"sh", "-c", `test -n "$DEC_EXEC_PROBE"`}
	}
	code, err := RunExecWithSecrets(ExecWithSecretsInput{
		ProjectRoot: root,
		Bundle:      "vikunja",
		Command:     command,
	})
	if err != nil {
		t.Fatalf("RunExecWithSecrets() err = %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
}

func TestAbsolutePath_WindowsSeparators(t *testing.T) {
	root := `D:\workspace\proj`
	target, err := secrets.NewBundleSyncTarget("tencent-cloud", "")
	if err != nil {
		t.Fatal(err)
	}
	abs, err := secrets.AbsolutePath(root, target, `env\tencent-cloud.env`)
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := filepath.Join(".secrets", "bundles", "tencent-cloud", "env", "tencent-cloud.env")
	if !strings.HasSuffix(abs, wantSuffix) {
		t.Fatalf("abs = %q, 应归一化 Windows 路径并落到 %q", abs, wantSuffix)
	}
}
