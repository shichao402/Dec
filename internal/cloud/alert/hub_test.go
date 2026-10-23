package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// 告警的关键行为：
//
//  1. 同类告警在窗口期内合并，不刷屏（需求 12.4）。
//  2. webhook 地址每次发送时才去取，换群立即生效。
//  3. 单台设备的告警游标只前进一次，不重复弹同一个通知。

// TestRaiseAndPending 验证告警能被设备取走，且不会重复取。
func TestRaiseAndPending(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := New(ctx, Options{})

	hub.Raise(KindBackupFailed, "冷备推送失败", "远端不可达")
	got := hub.Pending("dev-a")
	if len(got) != 1 {
		t.Fatalf("应有 1 条告警，实际 %d", len(got))
	}
	if got[0].Kind != KindBackupFailed {
		t.Fatalf("告警类别为 %q", got[0].Kind)
	}
	again := hub.Pending("dev-a")
	if len(again) != 0 {
		t.Fatalf("同一设备不应重复取到告警，实际 %d", len(again))
	}
	// 另一台设备仍应取到。
	if len(hub.Pending("dev-b")) != 1 {
		t.Fatal("另一台设备应能取到告警")
	}
}

// TestMergeSameKind 验证窗口期内的同类告警合并（需求 12.4）。
func TestMergeSameKind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := New(ctx, Options{MergeWindow: time.Minute})

	first := hub.Raise(KindEnroll, "设备登记尝试", "dev-x")
	for i := 0; i < 4; i++ {
		hub.Raise(KindEnroll, "设备登记尝试", "dev-y")
	}
	got := hub.Pending("dev-a")
	if len(got) != 1 {
		t.Fatalf("同类告警应合并成 1 条，实际 %d", len(got))
	}
	if got[0].Count != 5 {
		t.Fatalf("合并后 Count 应为 5，实际 %d", got[0].Count)
	}
	if got[0].ID != first.ID {
		t.Fatal("合并后应保持同一个 ID")
	}
}

// TestDifferentKindsNotMerged 验证不同类别不互相合并。
func TestDifferentKindsNotMerged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := New(ctx, Options{MergeWindow: time.Minute})

	hub.Raise(KindEnroll, "登记", "")
	hub.Raise(KindBackupFailed, "冷备", "")
	hub.Raise(KindRecovery, "恢复模式", "")

	if got := len(hub.Pending("dev-a")); got != 3 {
		t.Fatalf("不同类别应各自独立，实际 %d 条", got)
	}
}

// TestMergeWindowExpires 验证超出窗口后新建一条。
func TestMergeWindowExpires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := New(ctx, Options{MergeWindow: 10 * time.Millisecond})

	hub.Raise(KindDeploy, "部署失败", "")
	time.Sleep(30 * time.Millisecond)
	hub.Raise(KindDeploy, "部署失败", "")

	if got := len(hub.Pending("dev-a")); got != 2 {
		t.Fatalf("超出窗口应新建告警，实际 %d 条", got)
	}
}

// TestWebhookSent 验证 webhook 被真正调用，且内容包含标题。
func TestWebhookSent(t *testing.T) {
	var mu sync.Mutex
	var got string
	done := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MsgType string `json:"msgtype"`
			Text    struct {
				Content string `json:"content"`
			} `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		got = body.Text.Content
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		done <- struct{}{}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := New(ctx, Options{
		WebhookSecret: func() (string, error) { return srv.URL, nil },
	})

	hub.Raise(KindRecovery, "服务进入恢复模式", "根密钥缺失")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("webhook 未被调用")
	}

	mu.Lock()
	content := got
	mu.Unlock()
	if content == "" {
		t.Fatal("webhook 内容为空")
	}
	if !contains(content, "恢复模式") {
		t.Fatalf("webhook 内容应包含标题，实际 %q", content)
	}
}

// TestWebhookBusinessError 验证企业微信 errcode 非 0 被视为失败。
func TestWebhookBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer srv.Close()

	hub := New(context.Background(), Options{
		WebhookSecret: func() (string, error) { return srv.URL, nil },
	})
	a := Alert{ID: "al-1", Kind: KindDeploy, Title: "部署失败", At: time.Now()}
	if err := hub.send(context.Background(), a); err == nil {
		t.Fatal("errcode 非 0 应返回错误")
	}
}

// TestWebhookEmptyURL 验证没配 webhook 时安静跳过，不当作失败。
func TestWebhookEmptyURL(t *testing.T) {
	hub := New(context.Background(), Options{
		WebhookSecret: func() (string, error) { return "", nil },
	})
	a := Alert{ID: "al-1", Kind: KindDeploy, Title: "部署失败", At: time.Now()}
	if err := hub.send(context.Background(), a); err != nil {
		t.Fatalf("空地址应跳过而不是报错: %v", err)
	}
}

// TestRecentOrder 验证最近的告警排在前面。
func TestRecentOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := New(ctx, Options{MergeWindow: time.Nanosecond})

	hub.Raise(KindEnroll, "第一条", "")
	hub.Raise(KindBackupFailed, "第二条", "")
	hub.Raise(KindRecovery, "第三条", "")

	got := hub.Recent(10)
	if len(got) != 3 {
		t.Fatalf("应有 3 条，实际 %d", len(got))
	}
	if got[0].Title != "第三条" {
		t.Fatalf("最近的一条应为“第三条”，实际 %q", got[0].Title)
	}
}

// TestKeepLimit 验证内存里的告警数量有上限，不会无界增长。
func TestKeepLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := New(ctx, Options{MergeWindow: time.Nanosecond, Keep: 3})

	for i := 0; i < 10; i++ {
		hub.Raise(KindEnroll, "登记", "")
	}
	if got := len(hub.Recent(100)); got != 3 {
		t.Fatalf("应只保留 3 条，实际 %d", got)
	}
}

// TestPendingEmptyDevice 验证无设备身份时不产生游标。
func TestPendingEmptyDevice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := New(ctx, Options{})
	hub.Raise(KindEnroll, "登记", "")
	if got := hub.Pending(""); got != nil {
		t.Fatalf("空设备 ID 应返回 nil，实际 %d 条", len(got))
	}
}

// TestKindsStable 锁定告警类别集合。
//
// 类别是合并的依据，随意增删会让“合并”突然失效或把不相关的事合并在一起。
func TestKindsStable(t *testing.T) {
	want := []Kind{
		KindBackupFailed, KindDeploy, KindEnroll, KindLocalTLS,
		KindRecovery, KindRemoteRewritten, KindRenderSecret,
	}
	got := Kinds()
	if len(got) != len(want) {
		t.Fatalf("类别数量变化: %d，期望 %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 个类别为 %q，期望 %q", i, got[i], want[i])
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
