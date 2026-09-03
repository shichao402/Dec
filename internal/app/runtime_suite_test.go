package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeSuiteCachePathIsStable(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	got, err := suiteCacheDir("v1.2.3", "linux", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(os.Getenv("DEC_HOME"), "runtime-cache", "1.2.3", "linux-arm64")
	if got != want {
		t.Fatalf("cache path = %q, want %q", got, want)
	}
}

func TestCachedSuiteRequiresMatchingManifestAndHashes(t *testing.T) {
	dir := t.TempDir()
	for _, component := range runtimeSuiteNames {
		if err := os.WriteFile(filepath.Join(dir, component), []byte(component), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeSuiteManifest(dir, "v1.2.3", "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	if !verifyCachedSuite(dir, "v1.2.3", "linux", "amd64") {
		t.Fatal("完整且摘要匹配的缓存应通过")
	}
	if err := os.WriteFile(filepath.Join(dir, "dec-server"), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if verifyCachedSuite(dir, "v1.2.3", "linux", "amd64") {
		t.Fatal("被篡改的缓存不得通过")
	}
}

func TestRemotePushUsesShAndNoTargetNetworkInstaller(t *testing.T) {
	for _, forbidden := range []string{"curl", "bash"} {
		if strings.Contains(remoteProbeScript, forbidden) {
			t.Fatalf("远端探测不得再要求 %q", forbidden)
		}
	}
}

func TestRuntimeActivationScriptVerifiesAllComponentsAndRollsBack(t *testing.T) {
	hashes := make(map[string]string, len(runtimeSuiteNames))
	for index, component := range runtimeSuiteNames {
		hashes[component] = strings.Repeat(string(rune('a'+index)), 64)
	}
	script, err := runtimeActivationScript("v1.2.3", "1.2.3", hashes)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"curl", "bash", "install.sh", "tar "} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("激活脚本不得依赖 %q", forbidden)
		}
	}
	for _, component := range runtimeSuiteNames {
		if !strings.Contains(script, component+") expected="+hashes[component]) {
			t.Fatalf("激活脚本缺少 %s 摘要", component)
		}
		if !strings.Contains(script, `"$bin/$b" --version`) {
			t.Fatal("激活后必须逐组件复验版本")
		}
	}
	for _, want := range []string{
		"sha256sum",
		"shasum -a 256",
		"降级为四组件 --version 校验",
		"rollback_suite",
		`mv "$bin/$b" "$backup/$b"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("激活脚本缺少 %q", want)
		}
	}
}
