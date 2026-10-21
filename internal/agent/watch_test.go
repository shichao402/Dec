package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/proto"
)

// 落点变化的监听（需求 4.7）。
//
// 需求原文是“监听落点变化并上报，监听不可用时定期上报”。这里用轮询实现，
// 所以用例要证明的是“变化能在远小于长轮询超时的时间内被发现”。

func TestSameIndex(t *testing.T) {
	cases := []struct {
		name string
		a, b map[string]string
		want bool
	}{
		{"都为空", nil, nil, true},
		{"内容相同", map[string]string{"a": "h1"}, map[string]string{"a": "h1"}, true},
		{"键数不同", map[string]string{"a": "h1"}, map[string]string{}, false},
		{"内容不同", map[string]string{"a": "h1"}, map[string]string{"a": "h2"}, false},
		{"键不同", map[string]string{"a": "h1"}, map[string]string{"b": "h1"}, false},
	}
	for _, c := range cases {
		if got := sameIndex(c.a, c.b); got != c.want {
			t.Fatalf("%s：got %v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestHashIndexShape(t *testing.T) {
	idx := hashIndex([]proto.ManifestEntry{
		{ID: "a", Hash: "h1"},
		{ID: "b", Hash: "h2"},
	})
	if len(idx) != 2 || idx["a"] != "h1" || idx["b"] != "h2" {
		t.Fatalf("索引内容不符：%+v", idx)
	}
}

func TestWatchLocalDetectsChange(t *testing.T) {
	path := t.TempDir() + "/a.md"
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	checks := []proto.ManifestEntry{{
		ID:     "asset:woa:a",
		Format: proto.FormatWhole,
		Paths:  []string{path},
	}}

	s := &Syncer{opts: Options{WatchInterval: 10 * time.Millisecond, Logf: func(string, ...any) {}}}
	changed := make(chan struct{}, 1)
	ctx, cancel := newTestCtx(t)
	defer cancel()

	go s.watchLocal(ctx, checks, BuildManifest(checks), changed)

	// 改动文件，模拟用户在本机手改。
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatalf("改写文件失败: %v", err)
	}

	select {
	case <-changed:
	case <-time.After(3 * time.Second):
		t.Fatal("落点变化未被发现")
	}
}

func TestWatchLocalIgnoresNoChange(t *testing.T) {
	path := t.TempDir() + "/a.md"
	if err := os.WriteFile(path, []byte("same"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	checks := []proto.ManifestEntry{{
		ID:     "asset:woa:a",
		Format: proto.FormatWhole,
		Paths:  []string{path},
	}}

	s := &Syncer{opts: Options{WatchInterval: 10 * time.Millisecond, Logf: func(string, ...any) {}}}
	changed := make(chan struct{}, 1)
	ctx, cancel := newTestCtx(t)
	defer cancel()

	go s.watchLocal(ctx, checks, BuildManifest(checks), changed)

	// 文件没动就不该有任何通知：否则每轮都会立刻空转。
	select {
	case <-changed:
		t.Fatal("文件未改动时不应通知")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestWatchLocalWithoutChecksReturns(t *testing.T) {
	// 没有核对清单（刚下发、还没回执）时不该启动轮询，
	// 否则每台刚装好的机器都会空转一轮。
	s := &Syncer{opts: Options{WatchInterval: 10 * time.Millisecond}}
	changed := make(chan struct{}, 1)
	ctx, cancel := newTestCtx(t)
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.watchLocal(ctx, nil, nil, changed)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("空清单应立即返回")
	}
}

func TestWatchLocalStopsOnCancel(t *testing.T) {
	path := t.TempDir() + "/a.md"
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	checks := []proto.ManifestEntry{{
		ID:     "asset:woa:a",
		Format: proto.FormatWhole,
		Paths:  []string{path},
	}}
	s := &Syncer{opts: Options{WatchInterval: 10 * time.Millisecond}}
	changed := make(chan struct{}, 1)
	ctx, cancel := newTestCtx(t)

	done := make(chan struct{})
	go func() {
		s.watchLocal(ctx, checks, BuildManifest(checks), changed)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("取消后监听协程应退出")
	}
}

// newTestCtx 返回一个可取消的 context，供协程测试使用。
func newTestCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}
