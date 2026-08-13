package freshness

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadVersionMeta(t *testing.T) {
	t.Run("missing file returns nil without error", func(t *testing.T) {
		dir := t.TempDir()
		meta, err := LoadVersionMeta(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if meta != nil {
			t.Fatalf("expected nil meta for missing file, got %+v", meta)
		}
	})

	t.Run("parses well-formed version file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".dec"), 0755); err != nil {
			t.Fatal(err)
		}
		content := "commit: abcdef1234567890\npulled_at: \"2026-04-30T09:49:49+08:00\"\n"
		if err := os.WriteFile(filepath.Join(dir, ".dec", ".version"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		meta, err := LoadVersionMeta(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if meta == nil {
			t.Fatal("expected non-nil meta")
		}
		if meta.Commit != "abcdef1234567890" {
			t.Errorf("Commit = %q, want abcdef1234567890", meta.Commit)
		}
		if meta.PulledAt != "2026-04-30T09:49:49+08:00" {
			t.Errorf("PulledAt = %q, want 2026-04-30T09:49:49+08:00", meta.PulledAt)
		}
	})

	t.Run("malformed file gives empty commit but no error", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".dec"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".dec", ".version"), []byte("totally not yaml"), 0644); err != nil {
			t.Fatal(err)
		}
		meta, err := LoadVersionMeta(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if meta == nil {
			t.Fatal("expected non-nil meta")
		}
		if meta.Commit != "" {
			t.Errorf("expected empty Commit from malformed input, got %q", meta.Commit)
		}
	})
}

func TestShouldCheck(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")

	if !ShouldCheck(state, time.Hour) {
		t.Error("missing state file should allow check")
	}

	// 写一个当下的 mtime
	if err := os.WriteFile(state, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if ShouldCheck(state, time.Hour) {
		t.Error("freshly-touched state should throttle within window")
	}

	// 把 mtime 拉到 2h 前
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(state, past, past); err != nil {
		t.Fatal(err)
	}
	if !ShouldCheck(state, time.Hour) {
		t.Error("state older than window should allow check")
	}

	// interval <= 0 始终允许
	if !ShouldCheck(state, 0) {
		t.Error("interval <= 0 should always allow check")
	}
}

func TestRecordCheck(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "sub", "state")

	if err := RecordCheck(state); err != nil {
		t.Fatalf("RecordCheck failed: %v", err)
	}
	info, err := os.Stat(state)
	if err != nil {
		t.Fatalf("stat state failed: %v", err)
	}
	if time.Since(info.ModTime()) > 5*time.Second {
		t.Errorf("expected fresh mtime, got %v", info.ModTime())
	}

	// 第二次调用更新 mtime
	past := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(state, past, past); err != nil {
		t.Fatal(err)
	}
	if err := RecordCheck(state); err != nil {
		t.Fatalf("RecordCheck failed: %v", err)
	}
	info2, err := os.Stat(state)
	if err != nil {
		t.Fatal(err)
	}
	if !info2.ModTime().After(past.Add(time.Second)) {
		t.Errorf("expected mtime to be refreshed, got %v", info2.ModTime())
	}
}

func TestStateFilePath(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	p1, err := StateFilePath("/tmp/project-a")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := StateFilePath("/tmp/project-b")
	if err != nil {
		t.Fatal(err)
	}
	if p1 == p2 {
		t.Error("different project roots must produce different state files")
	}
	// 再次同路径应稳定
	p1Again, err := StateFilePath("/tmp/project-a")
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p1Again {
		t.Errorf("state file path for same project must be stable, %q vs %q", p1, p1Again)
	}
}

func TestIsDisabled(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"on":    false,
		"1":     false,
		"off":   true,
		"OFF":   true,
		"0":     true,
		"false": true,
		"no":    true,
	}
	for val, want := range cases {
		t.Setenv(EnvDisable, val)
		if got := IsDisabled(); got != want {
			t.Errorf("IsDisabled with %q = %v, want %v", val, got, want)
		}
	}
}

func TestInterval(t *testing.T) {
	t.Setenv(EnvInterval, "")
	if got := Interval(); got != DefaultInterval {
		t.Errorf("empty env should give default, got %v", got)
	}
	t.Setenv(EnvInterval, "30m")
	if got := Interval(); got != 30*time.Minute {
		t.Errorf("got %v, want 30m", got)
	}
	t.Setenv(EnvInterval, "garbage")
	if got := Interval(); got != DefaultInterval {
		t.Errorf("garbage env should fall back, got %v", got)
	}
	t.Setenv(EnvInterval, "-1h")
	if got := Interval(); got != DefaultInterval {
		t.Errorf("negative env should fall back, got %v", got)
	}
}

func TestShortHash(t *testing.T) {
	if got := ShortHash("abcdef1234567890"); got != "abcdef1" {
		t.Errorf("ShortHash = %q, want abcdef1", got)
	}
	if got := ShortHash("abc"); got != "abc" {
		t.Errorf("ShortHash = %q, want abc", got)
	}
	if got := ShortHash(""); got != "" {
		t.Errorf("ShortHash = %q, want empty", got)
	}
}

func TestFormatAndWriteHint(t *testing.T) {
	notStale := CheckResult{Stale: false, LocalCommit: "a", RemoteCommit: "b"}
	if FormatHint(notStale) != "" {
		t.Error("non-stale result should produce empty hint")
	}

	stale := CheckResult{
		Stale:        true,
		LocalCommit:  "abcdef1234",
		RemoteCommit: "9876543210",
	}
	hint := FormatHint(stale)
	if hint == "" {
		t.Fatal("stale result must produce non-empty hint")
	}

	var buf bytes.Buffer
	WriteHint(&buf, stale)
	if buf.Len() == 0 {
		t.Error("WriteHint should output for stale result")
	}

	buf.Reset()
	WriteHint(&buf, notStale)
	if buf.Len() != 0 {
		t.Errorf("WriteHint should stay silent for non-stale result, got %q", buf.String())
	}

	WriteHint(nil, stale) // 必须不 panic
}

func TestCheck_DisabledByEnv(t *testing.T) {
	t.Setenv(EnvDisable, "off")
	result := Check(context.Background(), t.TempDir())
	if !result.Skipped {
		t.Error("expected Skipped=true when disabled")
	}
	if result.Stale {
		t.Error("disabled check should never be stale")
	}
}

func TestCheck_NoVersionFile(t *testing.T) {
	t.Setenv(EnvDisable, "")
	t.Setenv("DEC_HOME", t.TempDir())
	result := Check(context.Background(), t.TempDir())
	if !result.Skipped {
		t.Error("expected Skipped=true when .dec/.version missing")
	}
}

func TestCheck_ThrottledByState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DEC_HOME", home)
	t.Setenv(EnvDisable, "")
	t.Setenv(EnvInterval, "24h")

	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".dec"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".dec", ".version"),
		[]byte("commit: deadbeef\npulled_at: \"2026-01-01T00:00:00Z\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	state, err := StateFilePath(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordCheck(state); err != nil {
		t.Fatal(err)
	}

	result := Check(context.Background(), projectRoot)
	if !result.Skipped {
		t.Error("expected Skipped=true when within throttle window")
	}
	if result.LocalCommit != "deadbeef" {
		t.Errorf("LocalCommit = %q, want deadbeef", result.LocalCommit)
	}
}
