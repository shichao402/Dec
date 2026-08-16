package handler

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestParseProcessorNoteName(t *testing.T) {
	tests := []struct {
		name               string
		wantInst, wantProc string
		ok                 bool
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

func TestGCMHandler_Apply(t *testing.T) {
	var calls []struct {
		stdin string
		args  []string
	}
	h := NewGCMHandler(func(_ context.Context, stdin string, args ...string) error {
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
host: cnb.cool
username: cnb
password: "secret-token"
`
	applied, err := ApplyNotes(context.Background(), reg, []Item{{
		Source:      SourceNote,
		Name:        ".gcm/cnb.yaml",
		NoteContent: content,
	}})
	if err != nil {
		t.Fatalf("ApplyNotes: %v", err)
	}
	if len(applied) != 1 || applied[0] != ".gcm/cnb.yaml" {
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

func TestGCMHandler_Revoke(t *testing.T) {
	var calls []struct {
		stdin string
		args  []string
	}
	h := NewGCMHandler(func(_ context.Context, stdin string, args ...string) error {
		calls = append(calls, struct {
			stdin string
			args  []string
		}{stdin: stdin, args: append([]string(nil), args...)})
		return nil
	})

	reg := NewRegistry()
	reg.Register(h)

	content := `
host: cnb.cool
username: cnb
password: "secret-token"
`
	revoked, err := RevokeNotes(context.Background(), reg, []Item{{
		Source:      SourceNote,
		Name:        ".gcm/cnb.yaml",
		NoteContent: content,
	}})
	if err != nil {
		t.Fatalf("RevokeNotes: %v", err)
	}
	if len(revoked) != 1 || revoked[0] != ".gcm/cnb.yaml" {
		t.Fatalf("revoked = %#v", revoked)
	}
	if len(calls) != 2 {
		t.Fatalf("git calls = %d, want 2: %#v", len(calls), calls)
	}
	if strings.Join(calls[0].args, " ") != "credential reject" {
		t.Fatalf("call[0] = %#v, want credential reject", calls[0].args)
	}
	if !strings.Contains(calls[0].stdin, "host=cnb.cool") || !strings.Contains(calls[0].stdin, "username=cnb") {
		t.Fatalf("reject stdin = %q", calls[0].stdin)
	}
	if strings.Contains(calls[0].stdin, "password=") {
		t.Fatalf("reject stdin 不应包含 password: %q", calls[0].stdin)
	}
	if strings.Join(calls[1].args, " ") != "config --global --unset credential.https://cnb.cool.provider" {
		t.Fatalf("call[1] = %#v, want config --global --unset ...", calls[1].args)
	}
}

func TestGCMHandler_RevokeToleratesMissingUnset(t *testing.T) {
	h := NewGCMHandler(func(_ context.Context, _ string, args ...string) error {
		if len(args) >= 2 && args[0] == "config" && args[1] == "--global" {
			return fmt.Errorf("exit status 5")
		}
		return nil
	})
	err := h.Revoke(context.Background(), Item{
		Name:        ".gcm/cnb.yaml",
		NoteContent: "host: cnb.cool\nusername: cnb\n",
	})
	if err != nil {
		t.Fatalf("Revoke 应容忍 --unset 失败, got %v", err)
	}
}

func TestGCMHandler_KindMismatch(t *testing.T) {
	h := NewGCMHandler(func(context.Context, string, ...string) error { return nil })
	err := h.Apply(context.Background(), Item{
		Name:        ".gcm/cnb.yaml",
		NoteContent: "kind: other\nhost: cnb.cool\nusername: cnb\npassword: x\n",
	})
	if err == nil || !strings.Contains(err.Error(), "期望 gcm") {
		t.Fatalf("want kind mismatch, got %v", err)
	}
}

func TestApplyNotes_SkipsOrdinaryEnv(t *testing.T) {
	called := false
	reg := NewRegistry()
	reg.Register(NewGCMHandler(func(context.Context, string, ...string) error {
		called = true
		return nil
	}))
	applied, err := ApplyNotes(context.Background(), reg, []Item{{
		Name:        ".env/foo.env",
		NoteContent: "A=1\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if called || len(applied) != 0 {
		t.Fatalf("should skip env note: called=%v applied=%v", called, applied)
	}
}

func TestMatchGCMPath(t *testing.T) {
	if !MatchGCMPath(".gcm/cnb.yaml") {
		t.Fatal("want match")
	}
	if MatchGCMPath("cnb_gitgcm.yaml") {
		t.Fatal("旧名不应再 Match")
	}
}
