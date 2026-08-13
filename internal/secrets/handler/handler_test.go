package handler

import (
	"context"
	"strings"
	"testing"
)

func TestParseProcessorNoteName(t *testing.T) {
	tests := []struct {
		name              string
		wantInst, wantProc string
		ok                bool
	}{
		{"cnb_gitgcm.yaml", "cnb", "gitgcm", true},
		{"handlers/cnb_gitgcm.yaml", "cnb", "gitgcm", true},
		{"cnb_gitgcm.yml", "cnb", "gitgcm", true},
		{"env/foo.env", "", "", false},
		{"gitgcm.yaml", "", "", false},
		{"_gitgcm.yaml", "", "", false},
		{"cnb_gitgcm", "", "", false},
		{"a_b_gitgcm.yaml", "a_b", "gitgcm", true},
	}
	for _, tc := range tests {
		inst, proc, ok := ParseProcessorNoteName(tc.name)
		if ok != tc.ok || inst != tc.wantInst || proc != tc.wantProc {
			t.Fatalf("%q => (%q,%q,%v), want (%q,%q,%v)",
				tc.name, inst, proc, ok, tc.wantInst, tc.wantProc, tc.ok)
		}
	}
}

func TestGitGCMHandler_Apply(t *testing.T) {
	var calls []struct {
		stdin string
		args  []string
	}
	h := NewGitGCMHandler(func(_ context.Context, stdin string, args ...string) error {
		cp := append([]string(nil), args...)
		calls = append(calls, struct {
			stdin string
			args  []string
		}{stdin: stdin, args: cp})
		return nil
	})

	reg := NewRegistry()
	reg.Register(h)

	content := `
kind: gitgcm
host: cnb.cool
username: cnb
password: "secret-token"
`
	applied, err := ApplyNotes(context.Background(), reg, []Item{{
		Source:      SourceNote,
		Name:        "cnb_gitgcm.yaml",
		NoteContent: content,
	}})
	if err != nil {
		t.Fatalf("ApplyNotes: %v", err)
	}
	if len(applied) != 1 || applied[0] != "cnb_gitgcm.yaml" {
		t.Fatalf("applied = %#v", applied)
	}
	if len(calls) != 2 {
		t.Fatalf("git calls = %d, want 2: %#v", len(calls), calls)
	}
	if calls[0].args[0] != "config" || !strings.Contains(strings.Join(calls[0].args, " "), "credential.https://cnb.cool.provider") {
		t.Fatalf("config call = %#v", calls[0].args)
	}
	if !strings.Contains(calls[1].stdin, "password=secret-token") || !strings.Contains(calls[1].stdin, "host=cnb.cool") {
		t.Fatalf("approve stdin = %q", calls[1].stdin)
	}
}

func TestGitGCMHandler_KindMismatch(t *testing.T) {
	h := NewGitGCMHandler(func(context.Context, string, ...string) error { return nil })
	err := h.Apply(context.Background(), Item{
		Name:        "cnb_gitgcm.yaml",
		NoteContent: "kind: other\nhost: cnb.cool\nusername: cnb\npassword: x\n",
	})
	if err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("want kind mismatch, got %v", err)
	}
}

func TestApplyNotes_SkipsOrdinaryEnv(t *testing.T) {
	called := false
	reg := NewRegistry()
	reg.Register(NewGitGCMHandler(func(context.Context, string, ...string) error {
		called = true
		return nil
	}))
	applied, err := ApplyNotes(context.Background(), reg, []Item{{
		Name:        "env/foo.env",
		NoteContent: "A=1\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if called || len(applied) != 0 {
		t.Fatalf("should skip env note: called=%v applied=%v", called, applied)
	}
}
