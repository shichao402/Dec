package agenttools

import (
	"encoding/json"
	"testing"
)

// Go 的 nil 切片会编成 null，而 Console（Rust）按 Vec 反序列化。
// 预检失败的 Plan 若带 steps:null 会被整条拒绝，错误文案随之丢失（ADR 0030）。
func TestPlanResultAlwaysSerializesStepsAsArray(t *testing.T) {
	cases := map[string]*PlanResult{
		"planFail":      planFail("dec_status", "预检失败"),
		"console owner": {Name: "dec_console_status", Owner: OwnerConsole, Shape: ShapeSingle},
	}
	for name, plan := range cases {
		raw, err := json.Marshal(plan)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var probe struct {
			Steps *[]Step `json:"steps"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if probe.Steps == nil {
			t.Fatalf("%s: steps 序列化为 null: %s", name, raw)
		}
	}
}
