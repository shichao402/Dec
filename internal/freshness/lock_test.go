package freshness

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireReleaseLock(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())

	release, err := acquireFreshnessLock()
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if release == nil {
		t.Fatal("first acquire should succeed (got nil release)")
	}
	release()

	// 释放后应能再次拿到
	release2, err := acquireFreshnessLock()
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if release2 == nil {
		t.Fatal("second acquire after release should succeed")
	}
	release2()
}

func TestAcquireFreshnessLock_Busy(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())

	release, err := acquireFreshnessLock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	// 未释放的情况下第二次抢应返回 (nil, nil)
	release2, err := acquireFreshnessLock()
	if err != nil {
		t.Errorf("busy acquire should not error, got %v", err)
	}
	if release2 != nil {
		t.Error("busy acquire should return nil release")
	}
}

func TestAcquireFreshnessLock_ReapsStale(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())

	path, err := freshnessLockPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	// 手造一个 11 分钟前的僵尸 lock
	if err := os.WriteFile(path, []byte("99999\n"), 0644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-11 * time.Minute)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	release, err := acquireFreshnessLock()
	if err != nil {
		t.Fatalf("acquire over stale lock: %v", err)
	}
	if release == nil {
		t.Fatal("stale lock should be reaped and allow acquire")
	}
	release()
}
