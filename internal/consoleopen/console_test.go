package consoleopen

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAvailableIsFalseUnderGoTest(t *testing.T) {
	if Available() {
		t.Fatal("test binaries must never launch Dec Console")
	}
}

func TestUnlockIntentIsAFlagNotAURL(t *testing.T) {
	if UnlockLocalFlag != "--unlock-local" {
		t.Fatalf("UnlockLocalFlag = %q", UnlockLocalFlag)
	}
	if strings.Contains(UnlockLocalFlag, "://") {
		t.Fatal("unlock intent must not be a URL")
	}
}

func TestFindConsoleExecutableUsesInstallLayout(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin install paths are under /Applications")
	}
	dir := t.TempDir()
	var installed string
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", dir)
		t.Setenv("ProgramFiles", filepath.Join(dir, "unused-pf"))
		t.Setenv("ProgramFiles(x86)", filepath.Join(dir, "unused-pf86"))
		installed = filepath.Join(dir, consoleName, consoleName+".exe")
	} else {
		t.Setenv("HOME", dir)
		installed = filepath.Join(dir, ".local/bin/dec-console")
	}
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := findConsoleExecutable()
	if err != nil {
		t.Fatal(err)
	}
	if got != installed {
		t.Fatalf("findConsoleExecutable() = %q, want %q", got, installed)
	}
}

func TestWindowsCandidatesIncludeLegacyAppBinary(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip()
	}
	var sawProduct, sawLegacy bool
	for _, path := range consoleInstallCandidates() {
		switch filepath.Base(path) {
		case "dec-console.exe":
			sawProduct = true
		case "app.exe":
			sawLegacy = true
		}
	}
	if !sawProduct || !sawLegacy {
		t.Fatalf("windows candidates must include dec-console.exe and app.exe: %v", consoleInstallCandidates())
	}
}
