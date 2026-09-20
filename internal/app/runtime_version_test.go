package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeGenerationForExecutableTracksMCPContent(t *testing.T) {
	dir := t.TempDir()
	serverPath := filepath.Join(dir, suiteBinaryName("dec-server", runtime.GOOS))
	mcpPath := filepath.Join(dir, suiteBinaryName("dec-mcp", runtime.GOOS))
	if err := os.WriteFile(mcpPath, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}

	first := runtimeGenerationForExecutable("v1.13.73", serverPath)
	if !strings.HasPrefix(first, "v1.13.73+") {
		t.Fatalf("first = %q", first)
	}
	if again := runtimeGenerationForExecutable("v1.13.73", serverPath); again != first {
		t.Fatalf("相同内容代号不稳定: %q != %q", again, first)
	}

	if err := os.WriteFile(mcpPath, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if second := runtimeGenerationForExecutable("v1.13.73", serverPath); second == first {
		t.Fatalf("同版本换二进制后代号未变化: %q", second)
	}
}

func TestRuntimeGenerationForExecutableFallsBackToVersion(t *testing.T) {
	got := runtimeGenerationForExecutable(" v1.13.73 ", filepath.Join(t.TempDir(), "dec-server"))
	if got != "v1.13.73" {
		t.Fatalf("got %q", got)
	}
}
