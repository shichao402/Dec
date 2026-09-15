package update

import (
	"encoding/json"
	"testing"
)

// helper 用 encoding/json 把检查结果写给壳。omitempty 会让空说明、非缓存结果整键消失，
// 壳侧 serde 以 missing field 失败。更新契约的 SSOT 仍在 relkit；在那边投影钉死之前，
// 这一层至少不能自己把键吃掉。
func TestConsoleCheckResultKeepsZeroValuedKeys(t *testing.T) {
	raw, err := json.Marshal(&ConsoleCheckResult{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{
		"currentVersion",
		"latestVersion",
		"needUpdate",
		"mandatory",
		"releaseNotesMarkdown",
		"releaseNotesUrl",
		"checkedAt",
		"fromCache",
		"autoCheckInterval",
		"canAutoInstall",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("空结果缺少键 %q；JSON = %s", key, raw)
		}
	}
}
