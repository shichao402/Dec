package app

import (
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
