package update

import (
	"slices"
	"strings"
	"testing"

	"go.firoyang.com/relkit/sdk"
)

func TestSuiteComponentsHaveExplicitRoles(t *testing.T) {
	want := []string{"dec-server", "dec-mcp", "dec-exec", "dec-host-setup"}
	if !slices.Equal(SuiteComponents, want) {
		t.Fatalf("SuiteComponents = %v, want %v", SuiteComponents, want)
	}
}

func TestSuiteRuntimeKeepsPinnedVersionEligible(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	code, err := sdk.SemverCode("v1.13.48")
	if err != nil {
		t.Fatal(err)
	}
	rt := fileSetRuntime("dec-server", "linux", "arm64", int64(code-1), t.TempDir())
	if rt.CurrentCode != int64(code-1) {
		t.Fatalf("CurrentCode = %d, want %d", rt.CurrentCode, code-1)
	}
	if rt.ClientSelectors["component"] != "dec-server" ||
		rt.ClientSelectors["os"] != "linux" ||
		rt.ClientSelectors["arch"] != "arm64" ||
		rt.ClientSelectors["audience"] != "runtime" {
		t.Fatalf("selectors 不正确: %#v", rt.ClientSelectors)
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
