package contribute

import (
	"strings"
	"testing"
)

func TestDraftDir(t *testing.T) {
	got := DraftDir("/tmp/proj", "relkit-ops")
	if !strings.Contains(got, "drafts") || !strings.Contains(got, "relkit-ops") {
		t.Fatalf("got %s", got)
	}
}

func TestProposeRejectsEmptyDiff(t *testing.T) {
	if _, err := Propose(t.Context(), Options{OriginRepo: "a/b", Diff: "  "}); err == nil {
		t.Fatal("expected error")
	}
}
