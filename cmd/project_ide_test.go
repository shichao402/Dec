package cmd

import "testing"

func TestUniqueProjectIDEs_DedupesSharedProjectLayouts(t *testing.T) {
	projectIDEs := uniqueProjectIDEs("/project", []string{"claude", "claude-internal", "codex", "codex-internal", "cursor"})
	got := projectIDENames(projectIDEs)
	want := []string{"claude", "codex", "cursor"}

	if len(got) != len(want) {
		t.Fatalf("项目级 IDE 数量 = %d, 期望 %d, 实际 %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("projectIDENames() = %#v, 期望 %#v", got, want)
		}
	}
}
