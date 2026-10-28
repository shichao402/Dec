package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlaintextMarkerMatchesProdScript 守住两边的明文前缀一致：
// 本机验收写进 secret 的前缀，必须正是线上验收在服务端搜的那个，
// 否则线上「无明文」永远通过，检查形同虚设（credential-landing 需求 8.4）。
func TestPlaintextMarkerMatchesProdScript(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "acceptance-prod.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), PlaintextMarker) {
		t.Fatalf("scripts/acceptance-prod.sh 没有搜索 %q", PlaintextMarker)
	}
}

func TestExpandSlot(t *testing.T) {
	home := homeDir()
	cases := map[string]string{
		"~/.ssh/dec_test_x":               filepath.Join(home, ".ssh", "dec_test_x"),
		"<p1>/.secrets/acceptance/t.json": filepath.Join("/tmp/proj", ".secrets", "acceptance", "t.json"),
	}
	for in, want := range cases {
		if got := expandSlot(in, "/tmp/proj"); got != want {
			t.Errorf("expandSlot(%q) = %q, want %q", in, got, want)
		}
	}
}
