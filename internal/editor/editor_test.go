package editor

import (
	"runtime"
	"testing"
)

func TestDefaultCommandNeverEmpty(t *testing.T) {
	got := DefaultCommand()
	if got == "" {
		t.Fatal("DefaultCommand() 不应返回空字符串")
	}
	if runtime.GOOS == "windows" && got != "notepad" {
		t.Fatalf("windows DefaultCommand() = %q, want notepad", got)
	}
}
