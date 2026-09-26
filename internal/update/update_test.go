package update

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManualInstallCommand(t *testing.T) {
	linuxCmd := manualInstallCommand("linux", false)
	if linuxCmd != "curl -fsSL https://cnb.cool/shichao402/Dec/-/git/raw/main/scripts/install.sh | bash" {
		t.Fatalf("linux 安装命令错误: %s", linuxCmd)
	}

	windowsCmd := manualInstallCommand("windows", false)
	if windowsCmd != "iwr -useb https://cnb.cool/shichao402/Dec/-/git/raw/main/scripts/install.ps1 | iex" {
		t.Fatalf("windows 安装命令错误: %s", windowsCmd)
	}
}

func TestMirrorInstallCommandUsesGitHubBackup(t *testing.T) {
	linuxCmd := manualInstallCommand("linux", true)
	if linuxCmd != "curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/main/scripts/install.sh | bash" {
		t.Fatalf("linux 镜像安装命令错误: %s", linuxCmd)
	}

	windowsCmd := manualInstallCommand("windows", true)
	if windowsCmd != "iwr -useb https://raw.githubusercontent.com/shichao402/Dec/main/scripts/install.ps1 | iex" {
		t.Fatalf("windows 镜像安装命令错误: %s", windowsCmd)
	}
}

func TestNetworkHelpMentionsUpdatesDomainAndProxy(t *testing.T) {
	help := NetworkHelp()
	for _, want := range []string{"updates.firoyang.com", "HTTPS_PROXY", "不必为此重装"} {
		if !strings.Contains(help, want) {
			t.Fatalf("排障建议缺少 %q:\n%s", want, help)
		}
	}
	for _, ban := range []string{"github.com", "install.sh", "install.ps1", "jsdelivr", "cnb.cool"} {
		if strings.Contains(help, ban) {
			t.Fatalf("NetworkHelp 不应再推销安装脚本/镜像 %q:\n%s", ban, help)
		}
	}
}

func TestDescribeRequestErrorStripsURL(t *testing.T) {
	timeoutErr := &url.Error{
		Op:  "Get",
		URL: "https://updates.firoyang.com/rup/directory/dec.pb",
		Err: timeoutError{},
	}
	got := describeRequestError(timeoutErr)
	if !strings.Contains(got, "请求超时") {
		t.Fatalf("超时描述 = %q, 期望包含 请求超时", got)
	}
	if strings.Contains(got, "updates.firoyang.com") {
		t.Fatalf("超时描述不应重复 URL: %q", got)
	}

	plainErr := &url.Error{Op: "Get", URL: "https://example.com", Err: errors.New("connection reset by peer")}
	if got := describeRequestError(plainErr); got != "connection reset by peer" {
		t.Fatalf("非超时描述 = %q", got)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string { return "context deadline exceeded" }
func (timeoutError) Timeout() bool { return true }

func TestUpdaterSelectsRuntimeAudience(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	rt := suiteRuntime("dec-server", runtime.GOOS, runtime.GOARCH, semverCodeOrZero("v1.13.48"), t.TempDir())
	if got := rt.ClientSelectors["audience"]; got != "runtime" {
		t.Fatalf("audience selector = %q, want runtime", got)
	}
}

func TestEntryURLsPointAtUpdateInternalDomain(t *testing.T) {
	urls := entryURLs()
	if len(urls) != 1 {
		t.Fatalf("entryURLs = %v", urls)
	}
	if !strings.Contains(urls[0], "update-internal.firoyang.com") {
		t.Fatalf("primary entry = %q", urls[0])
	}
	for _, u := range urls {
		if strings.Contains(u, "raw.firoyang.com") || strings.Contains(u, "raw2.firoyang.com") {
			t.Fatalf("cos raw planes must be gone: %v", urls)
		}
	}
}

func TestEmbeddedRecoveryFromRelkitJSON(t *testing.T) {
	help := embeddedRecovery()
	if help == nil || help.Message == "" || len(help.Links) < 2 {
		t.Fatalf("recovery: %+v", help)
	}
}

func TestEmbeddedRelkitMatchesRootSSOT(t *testing.T) {
	rootJSON, err := os.ReadFile(filepath.Join("..", "..", "relkit.json"))
	if err != nil {
		t.Fatalf("read repo-root relkit.json: %v (run tests from module root via go test ./internal/update/...)", err)
	}
	if !bytes.Equal(rootJSON, embeddedRelkitJSON) {
		t.Fatal("internal/update/embed/relkit.json 与根目录 relkit.json 不一致；请运行: go generate ./internal/update")
	}
}

func TestTrustedKeysFromEmbeddedRelkit(t *testing.T) {
	keys, err := trustedKeys()
	if err != nil {
		t.Fatalf("trustedKeys: %v", err)
	}
	pk, ok := keys[keyID]
	if !ok {
		t.Fatalf("missing keyId %q in TrustedKeys", keyID)
	}
	if len(pk) != ed25519.PublicKeySize {
		t.Fatalf("public key length = %d", len(pk))
	}

	var root relkitFile
	rootJSON, err := os.ReadFile(filepath.Join("..", "..", "relkit.json"))
	if err != nil {
		t.Fatalf("read repo-root relkit.json: %v", err)
	}
	if err := json.Unmarshal(rootJSON, &root); err != nil {
		t.Fatalf("parse root relkit.json: %v", err)
	}
	var wantB64 string
	for _, pkCfg := range root.Signing.PublicKeys {
		if pkCfg.KeyID == keyID {
			wantB64 = pkCfg.PublicKeyBase64
			break
		}
	}
	if wantB64 == "" {
		t.Fatalf("root relkit.json missing publicKeys entry for %q", keyID)
	}
	gotB64 := base64.StdEncoding.EncodeToString(pk)
	if gotB64 != wantB64 {
		t.Fatalf("dec-2026 public key = %q, want %q from root relkit.json", gotB64, wantB64)
	}
}
