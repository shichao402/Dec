package servicehost

import (
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/app"
)

// 需求 4.1 守护：pull 剪枝的每条事件都必须被保留，而非只保留最后一条汇总。
//
// eventCollector 是操作事件在单次调用内的统一收集器。若这里只保留末条，
// Console/Agent 就看不到被剪枝的每条路径，违背「删了什么、保留什么、为什么」。
func TestEventCollectorKeepsAllPruneEvents(t *testing.T) {
	c := &eventCollector{}
	prunePaths := []string{"a.env", "b.env", "c.env"}
	for _, p := range prunePaths {
		c.Emit(app.OperationEvent{
			Time:    time.Now(),
			Level:   app.EventInfo,
			Scope:   "pull.prune",
			Message: "已将孤儿 " + p + " 移入隔离区",
		})
	}
	// 任意普通事件也要保留，不覆盖剪枝明细。
	c.Emit(app.OperationEvent{Time: time.Now(), Level: app.EventWarn, Scope: "pull.reconcile", Message: "x"})

	if len(c.events) != 4 {
		t.Fatalf("应保留全部 4 条事件, got %d", len(c.events))
	}
	// 剪枝明细逐条可见。
	var pruneSeen []string
	for _, e := range c.events {
		if e.Scope == "pull.prune" {
			pruneSeen = append(pruneSeen, e.Message)
		}
	}
	if len(pruneSeen) != 3 {
		t.Fatalf("应保留 3 条剪枝明细, got %d: %#v", len(pruneSeen), pruneSeen)
	}
	for i, p := range prunePaths {
		want := "已将孤儿 " + p + " 移入隔离区"
		if pruneSeen[i] != want {
			t.Fatalf("明细 #%d 不符: got %q want %q", i, pruneSeen[i], want)
		}
	}
}
