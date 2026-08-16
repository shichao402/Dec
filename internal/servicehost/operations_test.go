package servicehost

import (
	"testing"

	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
)

func TestOperationBrokerProjectMutualExclusionAndWatch(t *testing.T) {
	broker := newOperationBroker()
	root := t.TempDir()

	first, err := broker.start(root, "pull", "mcp-1", "mcp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.start(root, "push", "tui-1", "tui"); err == nil {
		t.Fatal("同一 project 的第二个写操作应返回 busy")
	}

	history, live, cancel, err := broker.subscribe(root, first.meta.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(history) != 0 || live == nil {
		t.Fatalf("初始 watch = history %d, live %v", len(history), live != nil)
	}

	broker.publish(root, &servicev1.WatchOperationResponse{
		Event: &servicev1.OperationEvent{Scope: "pull", Message: "拉取中"},
	})
	event := <-live
	if event.Event == nil || event.Event.Message != "拉取中" {
		t.Fatalf("watch 未收到操作进度: %#v", event)
	}

	broker.finish(root, &servicev1.WatchOperationResponse{ResultJson: []byte(`{"ok":true}`)})
	final := <-live
	if !final.Done {
		t.Fatalf("watch 未收到完成事件: %#v", final)
	}

	if _, err := broker.start(root, "push", "tui-1", "tui"); err != nil {
		t.Fatalf("前一操作结束后应可启动新操作: %v", err)
	}
}

func TestOperationBrokerDifferentProjectsCanRunConcurrently(t *testing.T) {
	broker := newOperationBroker()
	if _, err := broker.start(t.TempDir(), "pull", "mcp", "mcp"); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.start(t.TempDir(), "push", "tui", "tui"); err != nil {
		t.Fatalf("不同 project 不应互斥: %v", err)
	}
	if op := broker.firstActive(); op == nil || !op.Active {
		t.Fatalf("firstActive 应返回活跃操作, got %#v", op)
	}
}
