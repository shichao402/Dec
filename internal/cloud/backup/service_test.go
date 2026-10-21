package backup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// 调度层的三个不变量：
//
//  1. 暂停期间不推送（需求 21.2）。
//  2. 恢复后立刻补推一次。
//  3. 失败上报，不静默。

// fakeReporter 记录上报事件。
type fakeReporter struct {
	mu       sync.Mutex
	failed   []string
	rewrite  []string
	checkeds []string
}

func (f *fakeReporter) BackupFailed(kind string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = append(f.failed, kind)
}

func (f *fakeReporter) RemoteRewritten(kind string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rewrite = append(f.rewrite, kind)
}

func (f *fakeReporter) BackupChecked(kind string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkeds = append(f.checkeds, kind)
}

func (f *fakeReporter) counts() (failed, rewrite, checked int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.failed), len(f.rewrite), len(f.checkeds)
}

// TestPauseStopsPush 验证暂停期间不推送。
func TestPauseStopsPush(t *testing.T) {
	r := newRemote(t)
	w := newWork(t)
	w.commit(t, "a.txt", "one")

	rep := &fakeReporter{}
	svc := NewService(Options{
		BackupTarget: Target{Name: "冷备", Dir: w.dir, URL: r.dir, Branch: "master"},
		Reporter:     rep,
		Debounce:     10 * time.Millisecond,
		DailyTick:    time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	svc.Pause()
	svc.Trigger()
	time.Sleep(100 * time.Millisecond)

	// 暂停期间远端应该还是空的。
	if _, err := r.repo.Reference("refs/heads/master", false); err == nil {
		t.Fatal("暂停期间不应推送")
	}

	svc.Resume()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := r.repo.Reference("refs/heads/master", false); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("恢复后应补推一次")
}

// TestPushFailureReported 验证推送失败会上报。
func TestPushFailureReported(t *testing.T) {
	w := newWork(t)
	w.commit(t, "a.txt", "one")

	rep := &fakeReporter{}
	svc := NewService(Options{
		// 远端指向不存在的路径：必然失败。
		BackupTarget: Target{Name: "冷备", Dir: w.dir, URL: "/nonexistent/repo.git", Branch: "master"},
		Reporter:     rep,
		Debounce:     10 * time.Millisecond,
		DailyTick:    time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	svc.Trigger()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if failed, _, _ := rep.counts(); failed > 0 {
			if svc.LastError() == nil {
				t.Fatal("失败后 LastError 不应为 nil")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("推送失败应上报")
}

// TestNoTargetIsSilent 验证未配置目标时不告警（本机调试的正常状态）。
func TestNoTargetIsSilent(t *testing.T) {
	rep := &fakeReporter{}
	svc := NewService(Options{
		Reporter:  rep,
		Debounce:  10 * time.Millisecond,
		DailyTick: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	svc.Trigger()
	time.Sleep(80 * time.Millisecond)

	if failed, _, _ := rep.counts(); failed != 0 {
		t.Fatalf("未配置目标时不应告警，实际 %d 次", failed)
	}
}

// TestTriggerAfterStopIgnored 验证 Stop 之后的触发被忽略。
func TestTriggerAfterStopIgnored(t *testing.T) {
	svc := NewService(Options{Debounce: time.Millisecond, DailyTick: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	svc.Stop()
	// 不应 panic，也不应阻塞。
	svc.Trigger()
	svc.Trigger()
}

// TestLastErrorInitiallyNil 验证未推送过时没有错误。
func TestLastErrorInitiallyNil(t *testing.T) {
	svc := NewService(Options{})
	if svc.LastError() != nil {
		t.Fatalf("初始不应有错误: %v", svc.LastError())
	}
	if svc.Paused() {
		t.Fatal("初始不应是暂停状态")
	}
}

// TestNeedsAuth 验证只有 SSH 地址才需要 key。
func TestNeedsAuth(t *testing.T) {
	cases := map[string]bool{
		"git@github.com:owner/repo.git": true,
		"ssh://git@github.com/owner/r":  true,
		"/tmp/local/repo.git":           false,
		"file:///tmp/local/repo.git":    false,
		"https://github.com/o/r.git":    false,
	}
	for url, want := range cases {
		if got := needsAuth(url); got != want {
			t.Fatalf("needsAuth(%q) = %v，期望 %v", url, got, want)
		}
	}
}

// TestEnsureRemoteNotConfigured 验证没有 URL 时明确报错。
func TestEnsureRemoteNotConfigured(t *testing.T) {
	w := newWork(t)
	p := &Pusher{Target: Target{Name: "冷备"}}
	if err := p.EnsureRemote(w.repo); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("应返回 ErrNotConfigured，实际 %v", err)
	}
	if _, err := p.Push(context.Background(), w.repo); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Push 也应返回 ErrNotConfigured，实际 %v", err)
	}
}
