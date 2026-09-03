package update

import (
	"strings"
	"testing"

	"cnb.cool/shichao402/relkit/sdk"
)

func TestSuiteUpdaterKeepsPinnedVersionEligible(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	updater, err := newUpdaterFor("v1.13.48", "dec-server", "linux", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	code, err := sdk.SemverCode("v1.13.48")
	if err != nil {
		t.Fatal(err)
	}
	if updater.CurrentCode != code-1 {
		t.Fatalf("CurrentCode = %d, want %d", updater.CurrentCode, code-1)
	}
	if updater.ClientSelectors["component"] != "dec-server" ||
		updater.ClientSelectors["os"] != "linux" ||
		updater.ClientSelectors["arch"] != "arm64" ||
		updater.ClientSelectors["audience"] != "runtime" {
		t.Fatalf("selectors 不正确: %#v", updater.ClientSelectors)
	}
}

func TestValidatePinnedSuiteVersionExplainsNewerChannelHead(t *testing.T) {
	if err := validatePinnedSuiteVersion("v1.13.48", "1.13.48"); err != nil {
		t.Fatalf("相同版本应通过: %v", err)
	}
	err := validatePinnedSuiteVersion("v1.13.48", "v1.13.49")
	if err == nil {
		t.Fatal("渠道 head 更高时必须拒绝")
	}
	for _, want := range []string{"渠道已有更高版本", "请先更新 Console", "预先准备该版本缓存"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误 %q 缺少 %q", err, want)
		}
	}
}

func TestValidatePinnedSuiteVersionReportsOtherMismatch(t *testing.T) {
	err := validatePinnedSuiteVersion("v1.13.48", "v1.13.47")
	if err == nil || !strings.Contains(err.Error(), "RUP 解析版本") {
		t.Fatalf("应报告普通版本不匹配，实际 %v", err)
	}
}
