package tui

import (
	"context"
	"testing"
	"time"

	"github.com/shichao402/Dec/pkg/app"
)

func TestAsyncLoad_BeginFinishAndStale(t *testing.T) {
	var load asyncLoad
	ctx1, gen1 := load.begin()
	if !load.busy() || gen1 != 1 {
		t.Fatalf("begin #1 busy=%v gen=%d", load.busy(), gen1)
	}
	ctx2, gen2 := load.begin()
	if gen2 != 2 {
		t.Fatalf("gen2 = %d", gen2)
	}
	select {
	case <-ctx1.Done():
	case <-time.After(time.Second):
		t.Fatal("被取代的上一代 ctx 应已 cancel")
	}
	if ctx2.Err() != nil {
		t.Fatal("当前代不应被 cancel")
	}
	if load.finish(gen1) {
		t.Fatal("过期 gen 不应 finish")
	}
	if !load.busy() {
		t.Fatal("过期 finish 后仍应 busy")
	}
	if !load.finish(gen2) {
		t.Fatal("当前 gen 应 finish")
	}
	if load.busy() {
		t.Fatal("finish 后不应 busy")
	}
}

func TestDeleteLoad_SurvivesPageLeaveAndDedups(t *testing.T) {
	oldOp := listDeleteCandidatesOperation
	defer func() { listDeleteCandidatesOperation = oldOp }()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	listDeleteCandidatesOperation = func(ctx context.Context, projectRoot string, includeRemote bool, reporter app.Reporter) ([]app.DeleteCandidate, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []app.DeleteCandidate{{Kind: app.DeleteKindBundle, BundleName: "demo", Label: "[bundle] demo"}}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 4 // Remote
	cmd := m.startDeleteCandidatesLoad(true, false)
	if cmd == nil {
		t.Fatal("首次加载应返回 cmd")
	}
	if !m.deleteLoad.busy() {
		t.Fatal("加载中应 busy")
	}
	if cmd2 := m.startDeleteCandidatesLoad(true, false); cmd2 != nil {
		t.Fatal("已在飞时应防重复，不再发 cmd")
	}

	// 切到 Home：不得取消在飞加载
	m.pageIndex = 0
	if leaveCmd := m.onPageChanged("Remote"); leaveCmd != nil {
		t.Fatal("离开 Delete 不应再触发新加载")
	}
	if !m.deleteLoad.busy() {
		t.Fatal("切页后加载应继续 busy")
	}

	msgCh := make(chan teaMsg, 1)
	go func() { msgCh <- cmd() }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("加载未启动")
	}
	close(release)
	var msg teaMsg
	select {
	case msg = <-msgCh:
	case <-time.After(2 * time.Second):
		t.Fatal("加载未完成")
	}
	loaded, ok := msg.(deleteLoadedMsg)
	if !ok {
		t.Fatalf("msg type = %T", msg)
	}
	if loaded.err != nil {
		t.Fatalf("loaded.err = %v", loaded.err)
	}

	updated, _ := m.Update(loaded)
	m = updated.(model)
	if m.deleteLoad.busy() {
		t.Fatal("完成后应清 busy")
	}
	if !m.deleteCandidatesLoaded || len(m.deleteCandidates) != 1 {
		t.Fatalf("切页期间完成的结果应被应用: loaded=%v n=%d", m.deleteCandidatesLoaded, len(m.deleteCandidates))
	}
}

// teaMsg 避免测试文件直接依赖 bubbletea 别名冲突时仍可用。
type teaMsg = any
