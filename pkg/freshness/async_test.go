package freshness

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withFetchStub 在单测里替换掉 fetchRemoteHead 包级变量，用 t.Cleanup 复原。
func withFetchStub(t *testing.T, stub func() (string, error)) {
	t.Helper()
	prev := fetchRemoteHead
	fetchRemoteHead = stub
	t.Cleanup(func() { fetchRemoteHead = prev })
}

func TestEmitCachedHintRespectsDisable(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	t.Setenv(EnvDisable, "off")
	project := t.TempDir()

	// 就算 cache 里写了 stale，也不应输出
	_ = WriteCachedResult(project, CachedResult{
		LocalCommit:  "aaa",
		RemoteCommit: "bbb",
		CheckedAt:    time.Now(),
	})

	var buf bytes.Buffer
	EmitCachedHint(&buf, project)
	if buf.Len() != 0 {
		t.Errorf("disabled emit should be silent, got %q", buf.String())
	}
}

func TestEmitCachedHintNoCache(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()

	var buf bytes.Buffer
	EmitCachedHint(&buf, project)
	if buf.Len() != 0 {
		t.Errorf("missing cache should be silent, got %q", buf.String())
	}
}

func TestEmitCachedHintStale(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()

	if err := WriteCachedResult(project, CachedResult{
		LocalCommit:  "aaaaaaaaaa",
		RemoteCommit: "bbbbbbbbbb",
		CheckedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	EmitCachedHint(&buf, project)
	out := buf.String()
	if out == "" {
		t.Fatal("stale cache should emit hint, got empty output")
	}
	// 不硬断文案，但本地和远端 commit 前缀应出现在提示里
	if !strings.Contains(out, "aaaaaaa") || !strings.Contains(out, "bbbbbbb") {
		t.Errorf("hint should reference both commits, got %q", out)
	}
}

func TestEmitCachedHintFresh(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()

	if err := WriteCachedResult(project, CachedResult{
		LocalCommit:  "same",
		RemoteCommit: "same",
		CheckedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	EmitCachedHint(&buf, project)
	if buf.Len() != 0 {
		t.Errorf("fresh cache should be silent, got %q", buf.String())
	}
}

func TestEmitCachedHintFetchError(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()

	// 上次 fetch 失败：Err 非空，不应打扰用户
	if err := WriteCachedResult(project, CachedResult{
		LocalCommit: "aaa",
		CheckedAt:   time.Now(),
		Err:         "network unreachable",
	}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	EmitCachedHint(&buf, project)
	if buf.Len() != 0 {
		t.Errorf("cached fetch error should be silent, got %q", buf.String())
	}
}

func TestRunBackgroundCheck_SkipsOnLockBusy(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()
	seedVersionFile(t, project, "local-commit")

	// 先把 lock 抢了，模拟 dec pull 在跑
	release, err := acquireFreshnessLock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	fetchCalled := false
	withFetchStub(t, func() (string, error) {
		fetchCalled = true
		return "remote-commit", nil
	})

	if err := RunBackgroundCheck(project); err != nil {
		t.Fatalf("busy lock path should not error, got %v", err)
	}
	if fetchCalled {
		t.Error("fetch should not be called when lock is held by someone else")
	}
	// cache 不应被创建
	path, _ := CacheFilePath(project)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("cache should not be created when lock is busy, got %v", err)
	}
}

func TestRunBackgroundCheck_SkipsOnThrottle(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()
	seedVersionFile(t, project, "local-commit")

	// 造一个最近刚 check 过的 state file，让 ShouldCheck 返回 false
	stateFile, err := StateFilePath(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := RecordCheck(stateFile); err != nil {
		t.Fatal(err)
	}

	fetchCalled := false
	withFetchStub(t, func() (string, error) {
		fetchCalled = true
		return "remote-commit", nil
	})

	if err := RunBackgroundCheck(project); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetchCalled {
		t.Error("fetch should be throttled, but fetchRemoteHead was called")
	}
	cachePath, _ := CacheFilePath(project)
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Errorf("throttled run should not write cache, got stat err=%v", err)
	}
}

func TestRunBackgroundCheck_WritesCacheOnFetch(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	// 把 interval 拉短以避免 state file 初始 mtime 误伤（其实 ShouldCheck 对无文件返回 true）
	project := t.TempDir()
	seedVersionFile(t, project, "abc1234")

	withFetchStub(t, func() (string, error) {
		return "def5678", nil
	})

	if err := RunBackgroundCheck(project); err != nil {
		t.Fatalf("RunBackgroundCheck: %v", err)
	}

	got, err := ReadCachedResult(project)
	if err != nil {
		t.Fatalf("ReadCachedResult: %v", err)
	}
	if got == nil {
		t.Fatal("expected cache to be written")
	}
	if got.LocalCommit != "abc1234" {
		t.Errorf("LocalCommit = %q, want abc1234", got.LocalCommit)
	}
	if got.RemoteCommit != "def5678" {
		t.Errorf("RemoteCommit = %q, want def5678", got.RemoteCommit)
	}
	if got.Err != "" {
		t.Errorf("Err should be empty on success, got %q", got.Err)
	}
	// throttle state 也应被更新
	stateFile, _ := StateFilePath(project)
	if _, err := os.Stat(stateFile); err != nil {
		t.Errorf("state file should be touched, got %v", err)
	}
}

func TestRunBackgroundCheck_RecordsFetchError(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()
	seedVersionFile(t, project, "abc1234")

	withFetchStub(t, func() (string, error) {
		return "", errors.New("boom")
	})

	if err := RunBackgroundCheck(project); err != nil {
		t.Fatalf("RunBackgroundCheck: %v", err)
	}
	got, err := ReadCachedResult(project)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("cache should be written even on fetch error so we can throttle retries")
	}
	if got.Err == "" {
		t.Error("Err should be populated on fetch failure")
	}
	// EmitCachedHint 在 Err 非空时应该静默
	var buf bytes.Buffer
	EmitCachedHint(&buf, project)
	if buf.Len() != 0 {
		t.Errorf("cached fetch error should be silent in EmitCachedHint, got %q", buf.String())
	}
}

func TestRunBackgroundCheck_DisabledIsNoop(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	t.Setenv(EnvDisable, "off")
	project := t.TempDir()
	seedVersionFile(t, project, "abc")

	fetchCalled := false
	withFetchStub(t, func() (string, error) {
		fetchCalled = true
		return "def", nil
	})

	if err := RunBackgroundCheck(project); err != nil {
		t.Fatal(err)
	}
	if fetchCalled {
		t.Error("disabled run must not call fetchRemoteHead")
	}
}

func TestRunBackgroundCheck_SkipsWhenNotPulled(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	project := t.TempDir()
	// 故意不 seedVersionFile：项目没 pull 过

	fetchCalled := false
	withFetchStub(t, func() (string, error) {
		fetchCalled = true
		return "", nil
	})

	if err := RunBackgroundCheck(project); err != nil {
		t.Fatal(err)
	}
	if fetchCalled {
		t.Error("projects without .dec/.version should not trigger fetch")
	}
	cachePath, _ := CacheFilePath(project)
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Errorf("should not write cache for un-pulled project, got %v", err)
	}
}
