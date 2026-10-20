package update

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
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

func TestClientProfileComesFromRuntimeBootstrap(t *testing.T) {
	old := runtimeBootstrapBase64
	t.Cleanup(func() { runtimeBootstrapBase64 = old })
	digest := "sha256:" + strings.Repeat("a", 64)
	source := fmt.Sprintf(`{
		"schema":"relkit.runtime-bootstrap/1",
		"resolutionDigest":%q,
		"profileDigest":%q,
		"rootFingerprint":"sha256:64eba24757a5e1574e6cabf8086284d90e5ff57ef0b821a50c3267263c3fd75d",
		"profile":{
			"product":"dec",
			"allowedChannels":["stable"],
			"directoryBootstrap":{
				"entryUrls":["https://update-internal.firoyang.com/directory/dec.pb"],
				"recovery":{"message":"offline","links":[{"label":"release","url":"https://example.com/"}]}
			},
			"digest":%q
		},
		"trustedKeys":[{"keyId":"dec-2026","key":"zVkesjz/3BhLrZ9qCvSJN0OdrIsePL4+v6AI9CCtio4="}]
	}`, digest, digest, digest)
	runtimeBootstrapBase64 = base64.StdEncoding.EncodeToString([]byte(source))
	profile, err := clientProfile()
	if err != nil {
		t.Fatal(err)
	}
	if profile.GetProduct() != "dec" {
		t.Fatalf("product = %q", profile.GetProduct())
	}
	if got := profile.GetEntryUrls(); len(got) != 1 || !strings.Contains(got[0], "update-internal.firoyang.com") {
		t.Fatalf("entry urls = %v", got)
	}
	if profile.GetRecovery().GetMessage() != "offline" {
		t.Fatalf("recovery = %+v", profile.GetRecovery())
	}
}
