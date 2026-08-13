package freshness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedVersionFile 造出一个带 .dec/.version 的项目目录，方便下游测试复用。
func seedVersionFile(t *testing.T, projectRoot, commit string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".dec"), 0755); err != nil {
		t.Fatal(err)
	}
	content := "commit: " + commit + "\npulled_at: \"2026-04-30T00:00:00Z\"\n"
	if err := os.WriteFile(filepath.Join(projectRoot, ".dec", ".version"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCacheRoundtrip(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()

	want := CachedResult{
		LocalCommit:  "abc1234",
		RemoteCommit: "def5678",
		CheckedAt:    time.Now().UTC().Truncate(time.Second),
	}
	if err := WriteCachedResult(project, want); err != nil {
		t.Fatalf("WriteCachedResult: %v", err)
	}
	got, err := ReadCachedResult(project)
	if err != nil {
		t.Fatalf("ReadCachedResult: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil cache")
	}
	if got.LocalCommit != want.LocalCommit || got.RemoteCommit != want.RemoteCommit {
		t.Errorf("commit mismatch: got %+v want %+v", got, want)
	}
	if !got.CheckedAt.Equal(want.CheckedAt) {
		t.Errorf("CheckedAt mismatch: got %v want %v", got.CheckedAt, want.CheckedAt)
	}
}

func TestReadCachedResult_Missing(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	got, err := ReadCachedResult(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing cache, got %+v", got)
	}
}

func TestReadCachedResult_Expired(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	t.Setenv(EnvInterval, "1h")
	project := t.TempDir()

	stale := CachedResult{
		LocalCommit:  "aaa",
		RemoteCommit: "bbb",
		CheckedAt:    time.Now().Add(-2 * time.Hour),
	}
	if err := WriteCachedResult(project, stale); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCachedResult(project)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expired cache should read as nil, got %+v", got)
	}
}

func TestReadCachedResult_CorruptFile(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()

	path, err := CacheFilePath(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCachedResult(project)
	if err != nil {
		t.Fatalf("corrupt cache should not return error, got %v", err)
	}
	if got != nil {
		t.Errorf("corrupt cache should read as nil, got %+v", got)
	}
}

func TestCachedResult_IsStale(t *testing.T) {
	cases := []struct {
		name  string
		local string
		remote string
		want  bool
	}{
		{"empty local", "", "bbb", false},
		{"empty remote", "aaa", "", false},
		{"both empty", "", "", false},
		{"equal", "aaa", "aaa", false},
		{"different", "aaa", "bbb", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := CachedResult{LocalCommit: c.local, RemoteCommit: c.remote}
			if got := r.IsStale(); got != c.want {
				t.Errorf("IsStale = %v, want %v", got, c.want)
			}
		})
	}
}

func TestInvalidateCache(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()

	// 无文件也不该报错
	if err := InvalidateCache(project); err != nil {
		t.Errorf("InvalidateCache on missing file should be nil, got %v", err)
	}

	// 写一个，删掉
	if err := WriteCachedResult(project, CachedResult{LocalCommit: "a", RemoteCommit: "b", CheckedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := InvalidateCache(project); err != nil {
		t.Fatal(err)
	}
	path, _ := CacheFilePath(project)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("cache should be gone, got stat err=%v", err)
	}
}

func TestWriteCachedResult_AtomicRename(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()

	r := CachedResult{LocalCommit: "a", RemoteCommit: "b", CheckedAt: time.Now()}
	if err := WriteCachedResult(project, r); err != nil {
		t.Fatal(err)
	}
	path, _ := CacheFilePath(project)
	// tmp 文件不应残留
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file should be renamed away, got %v", err)
	}
	// 文件应该是合法 JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed CachedResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("cache should be valid JSON: %v", err)
	}
}
