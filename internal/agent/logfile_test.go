package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 日志只写一个文件，并且会追加而不是覆盖（需求 5.1）。
func TestLogFileSingleFileAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", LogFileName)

	l := OpenLogFile(LogFileOptions{Path: path})
	l.Printf("第一条 %d", 1)
	_ = l.Close()

	l = OpenLogFile(LogFileOptions{Path: path})
	l.Printf("第二条")
	_ = l.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "第一条 1") || !strings.Contains(s, "第二条") {
		t.Fatalf("日志内容不完整: %q", s)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "logs"))
	if len(entries) != 1 {
		t.Fatalf("应只有一个日志文件，实际 %d 个", len(entries))
	}
}

// 超过阈值轮转，并且只保留 Keep 份历史（需求 5.2）。
func TestLogFileRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LogFileName)
	l := OpenLogFile(LogFileOptions{Path: path, MaxBytes: 100, Keep: 2})
	defer l.Close()

	line := strings.Repeat("x", 60) + "\n"
	for i := 0; i < 10; i++ {
		_, _ = l.Write([]byte(line))
	}

	for _, name := range []string{LogFileName, LogFileName + ".1", LogFileName + ".2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("缺少 %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, LogFileName+".3")); !os.IsNotExist(err) {
		t.Fatalf("超出保留份数的历史文件应被删除")
	}
	info, _ := os.Stat(path)
	if info.Size() > 100 {
		t.Fatalf("当前文件 %d 字节，超过阈值", info.Size())
	}
}

// 目录不可写时退回 fallback 继续工作，不 panic、不退出（需求 5.5）。
func TestLogFileFallbackWhenUnwritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上目录权限语义不同")
	}
	if os.Geteuid() == 0 {
		t.Skip("root 无视权限位")
	}
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(ro, 0o700)

	var buf bytes.Buffer
	l := OpenLogFile(LogFileOptions{Path: filepath.Join(ro, "logs", LogFileName), Fallback: &buf})
	l.Printf("还在工作")
	if !strings.Contains(buf.String(), "还在工作") {
		t.Fatalf("fallback 没收到日志: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "日志文件不可写") {
		t.Fatalf("应说明退回原因: %q", buf.String())
	}
}

// Echo 用于前台运行时在终端里也看得到。
func TestLogFileEcho(t *testing.T) {
	var echo bytes.Buffer
	l := OpenLogFile(LogFileOptions{Path: filepath.Join(t.TempDir(), LogFileName), Echo: &echo})
	defer l.Close()
	l.Printf("hello")
	if !strings.Contains(echo.String(), "[dec-agent] hello") {
		t.Fatalf("echo 内容不对: %q", echo.String())
	}
}
