package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLocatePKV_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: PATH/exec 语义与 POSIX 不同")
	}

	tmpDir := t.TempDir()
	fakePKV := filepath.Join(tmpDir, "pkv")
	if err := os.WriteFile(fakePKV, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("写入伪 pkv 失败: %v", err)
	}
	t.Setenv("PATH", tmpDir)

	cmd, err := LocatePKV("arg1", "arg2")
	if err != nil {
		t.Fatalf("预期成功，却返回错误: %v", err)
	}
	if cmd == nil {
		t.Fatal("预期返回非 nil 的 *exec.Cmd")
	}
	if cmd.Path != fakePKV {
		t.Errorf("cmd.Path = %q, 期望 %q", cmd.Path, fakePKV)
	}
	// exec.Command 的 Args[0] 通常是可执行名或路径，按 Go 标准库实现应为 path
	if len(cmd.Args) != 3 {
		t.Errorf("cmd.Args 长度 = %d, 期望 3 (path + arg1 + arg2)，实际 %v", len(cmd.Args), cmd.Args)
	}
	if cmd.Stdin != os.Stdin {
		t.Error("cmd.Stdin 未绑定到 os.Stdin")
	}
	if cmd.Stdout != os.Stdout {
		t.Error("cmd.Stdout 未绑定到 os.Stdout")
	}
	if cmd.Stderr != os.Stderr {
		t.Error("cmd.Stderr 未绑定到 os.Stderr")
	}
}

func TestLocatePKV_NotFound(t *testing.T) {
	// 空 PATH 一定查不到 pkv
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)

	cmd, err := LocatePKV()
	if err == nil {
		t.Fatal("预期返回错误，实际为 nil")
	}
	if cmd != nil {
		t.Errorf("预期 cmd 为 nil，实际 %v", cmd)
	}
	if !strings.Contains(err.Error(), "未找到 pkv") {
		t.Errorf("错误消息应含中文 '未找到 pkv'，实际: %q", err.Error())
	}
}

func TestLocatePKVWithEnv_AppendsEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: PATH/exec 语义与 POSIX 不同")
	}

	tmpDir := t.TempDir()
	fakePKV := filepath.Join(tmpDir, "pkv")
	if err := os.WriteFile(fakePKV, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("写入伪 pkv 失败: %v", err)
	}
	t.Setenv("PATH", tmpDir)

	cmd, err := LocatePKVWithEnv([]string{"BW_SESSION=test-session"}, "get", "all", "demo")
	if err != nil {
		t.Fatalf("预期成功，却返回错误: %v", err)
	}
	if cmd == nil {
		t.Fatal("预期返回非 nil 的 *exec.Cmd")
	}

	// cmd.Env 应当包含 BW_SESSION=test-session，且至少包含 os.Environ() 的一些基础变量
	hasSession := false
	hasPATH := false
	for _, kv := range cmd.Env {
		if kv == "BW_SESSION=test-session" {
			hasSession = true
		}
		if strings.HasPrefix(kv, "PATH=") {
			hasPATH = true
		}
	}
	if !hasSession {
		t.Errorf("cmd.Env 应包含 BW_SESSION=test-session，实际: %v", cmd.Env)
	}
	if !hasPATH {
		t.Errorf("cmd.Env 应继承 PATH，实际: %v", cmd.Env)
	}
	// std streams 行为应和 LocatePKV 一致
	if cmd.Stdin != os.Stdin || cmd.Stdout != os.Stdout || cmd.Stderr != os.Stderr {
		t.Error("LocatePKVWithEnv 未保留 LocatePKV 的 std stream 绑定")
	}
}

func TestLocatePKVWithEnv_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)

	cmd, err := LocatePKVWithEnv([]string{"BW_SESSION=x"})
	if err == nil {
		t.Fatal("预期返回错误，实际为 nil")
	}
	if cmd != nil {
		t.Errorf("预期 cmd 为 nil，实际 %v", cmd)
	}
}

func TestBuildPKVUnlockCmd_CapturesStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: PATH/exec 语义与 POSIX 不同")
	}

	tmpDir := t.TempDir()
	fakePKV := filepath.Join(tmpDir, "pkv")
	// 伪 pkv unlock: stdout 打印 session，stderr 打印诊断，返回 0
	script := "#!/bin/sh\n" +
		"printf 'session-abc\\n'\n" +
		"printf 'diagnostic noise\\n' 1>&2\n" +
		"exit 0\n"
	if err := os.WriteFile(fakePKV, []byte(script), 0o755); err != nil {
		t.Fatalf("写入伪 pkv 失败: %v", err)
	}
	t.Setenv("PATH", tmpDir)

	// 强制走 console 不可用的 fallback 路径，stdin/stderr 应保持 os.Stdin/os.Stderr，
	// 这样 cmd.Run() 才能直接拿到测试进程的句柄，stdout buffer 才能稳定捕获。
	prev := openTTYFunc
	t.Cleanup(func() { openTTYFunc = prev })
	openTTYFunc = func() (*os.File, *os.File, error) { return nil, nil, errors.New("tty unavailable in test") }

	cmd, buf, cleanup, err := BuildPKVUnlockCmd()
	if err != nil {
		t.Fatalf("BuildPKVUnlockCmd 返回错误: %v", err)
	}
	t.Cleanup(cleanup)
	if cmd == nil || buf == nil {
		t.Fatal("cmd / buf 不应为 nil")
	}

	// Args[1] 应当是 "unlock"
	if len(cmd.Args) < 2 || cmd.Args[1] != "unlock" {
		t.Errorf("cmd.Args 应为 [path, \"unlock\"]，实际 %v", cmd.Args)
	}
	// fallback 下 Stdin/Stderr 维持 os.Stdin/os.Stderr
	if cmd.Stdin != os.Stdin {
		t.Error("cmd.Stdin 未绑定到 os.Stdin")
	}
	if cmd.Stderr != os.Stderr {
		t.Error("cmd.Stderr 未绑定到 os.Stderr")
	}
	if cmd.Stdout != buf {
		t.Error("cmd.Stdout 未绑定到返回的 *bytes.Buffer")
	}

	// 实际跑一次，buffer 应当拿到 stdout 内容，而 stderr 内容不会进 buffer
	if err := cmd.Run(); err != nil {
		t.Fatalf("伪 pkv cmd.Run 失败: %v", err)
	}
	got := buf.String()
	if got != "session-abc\n" {
		t.Errorf("buffer = %q，期望 %q", got, "session-abc\n")
	}
}

func TestBuildPKVUnlockCmd_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)

	cmd, buf, cleanup, err := BuildPKVUnlockCmd()
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("预期返回错误，实际为 nil")
	}
	if cmd != nil || buf != nil {
		t.Errorf("未找到 pkv 时 cmd/buf 应为 nil，实际 cmd=%v buf=%v", cmd, buf)
	}
}

func TestParsePKVUnlockOutput(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "带末尾换行", input: "session-xyz\n", want: "session-xyz"},
		{name: "两侧空白", input: "  session-xyz  \n", want: "session-xyz"},
		{name: "中间含空白视为非法", input: "session-xyz something\n", wantErr: true},
		{name: "多行视为非法", input: "session-xyz\nmore\n", wantErr: true},
		{name: "空串", input: "", wantErr: true},
		{name: "仅空白", input: "   \n\t\n", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := bytes.NewBufferString(tc.input)
			got, err := ParsePKVUnlockOutput(buf)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("预期返回错误，实际 got=%q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("预期成功，返回错误: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q，期望 %q", got, tc.want)
			}
		})
	}
}

func TestParsePKVUnlockOutput_NilBuffer(t *testing.T) {
	got, err := ParsePKVUnlockOutput(nil)
	if err == nil {
		t.Fatalf("nil buffer 应返回错误，got=%q", got)
	}
}

// TestLocatePKVInteractive_TTYAttached 验证：openTTYFunc 返回有效 *os.File 时，
// LocatePKVInteractive 会把 cmd.Stdin / cmd.Stderr 切到该 tty 文件，stdout 保持 os.Stdout。
// 拿一个临时 regular file 当 tty 替身（attachTTYIfAvailable 不关心是不是真 tty，只关心非 nil）。
func TestLocatePKVInteractive_TTYAttached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: PATH/exec 语义与 POSIX 不同")
	}

	tmpDir := t.TempDir()
	fakePKV := filepath.Join(tmpDir, "pkv")
	if err := os.WriteFile(fakePKV, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("写入伪 pkv 失败: %v", err)
	}
	t.Setenv("PATH", tmpDir)

	// 临时 file 作为 tty 替身。test cleanup 会确认它被 attachTTYIfAvailable 关闭。
	// 这里 in == out（模拟 Unix 一个 fd 双向用），cleanup 应只关一次而不是 double-close。
	stub, err := os.CreateTemp(tmpDir, "fake-tty-*")
	if err != nil {
		t.Fatalf("创建 stub tty 失败: %v", err)
	}
	prev := openTTYFunc
	t.Cleanup(func() { openTTYFunc = prev })
	openTTYFunc = func() (*os.File, *os.File, error) { return stub, stub, nil }

	cmd, cleanup, err := LocatePKVInteractive("unlock")
	if err != nil {
		t.Fatalf("LocatePKVInteractive 返回错误: %v", err)
	}
	if cmd == nil {
		t.Fatal("cmd 不应为 nil")
	}
	if cmd.Stdin != stub {
		t.Errorf("cmd.Stdin 应为 stub tty, 实际 %v", cmd.Stdin)
	}
	if cmd.Stderr != stub {
		t.Errorf("cmd.Stderr 应为 stub tty, 实际 %v", cmd.Stderr)
	}
	if cmd.Stdout != os.Stdout {
		t.Errorf("cmd.Stdout 应保持 os.Stdout, 实际 %v", cmd.Stdout)
	}
	cleanup()
	// cleanup 后 stub 被关闭：再写入应失败
	if _, err := stub.Write([]byte("x")); err == nil {
		t.Error("cleanup 后 stub 仍可写，说明未关闭 tty 文件句柄")
	}
}

// TestLocatePKVInteractive_TTYUnavailable 验证：openTTYFunc 失败时回退到 os.Stdin/os.Stderr，
// cleanup 是 no-op，且不返回 error（这是日常 daemon / Windows 场景的兜底）。
func TestLocatePKVInteractive_TTYUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: PATH/exec 语义与 POSIX 不同")
	}

	tmpDir := t.TempDir()
	fakePKV := filepath.Join(tmpDir, "pkv")
	if err := os.WriteFile(fakePKV, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("写入伪 pkv 失败: %v", err)
	}
	t.Setenv("PATH", tmpDir)

	prev := openTTYFunc
	t.Cleanup(func() { openTTYFunc = prev })
	openTTYFunc = func() (*os.File, *os.File, error) { return nil, nil, errors.New("no tty") }

	cmd, cleanup, err := LocatePKVInteractive("unlock")
	if err != nil {
		t.Fatalf("LocatePKVInteractive 不应返回错误（应回退）: %v", err)
	}
	t.Cleanup(cleanup)
	if cmd.Stdin != os.Stdin {
		t.Errorf("回退路径 cmd.Stdin 应为 os.Stdin, 实际 %v", cmd.Stdin)
	}
	if cmd.Stderr != os.Stderr {
		t.Errorf("回退路径 cmd.Stderr 应为 os.Stderr, 实际 %v", cmd.Stderr)
	}
}
