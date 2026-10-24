package audit

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// gc 与并发写入的交互。
//
// 这是 gc 最容易出问题的地方：回收要删掉旧 pack，而提交要读 HEAD tree 里的对象。
// 两者撞在一起时，提交会用到一个正在被删除的 pack。这里的取舍是
// 「gc 期间只 fsync、不 commit，等 gc 结束换新句柄后一起提交」——
// 数据不丢（记录已落盘），只是晚几秒进历史。

// TestWritesDuringGcAreKept 验证 gc 期间写入的记录不会丢。
func TestWritesDuringGcAreKept(t *testing.T) {
	const base = 15
	log, at := writeForGc(t, base)

	// gc 与写入并发跑起来。
	var wg sync.WaitGroup
	gcErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		gcErr <- log.gcAt(1)
	}()

	// 在 gc 跑的同一时间窗口里持续写入。
	const during = 30
	for i := 0; i < during; i++ {
		if err := log.Append(Entry{
			At:      at.Add(time.Duration(i) * time.Second),
			Event:   EventAssetWrite,
			Subject: fmt.Sprintf("during-%d", i),
			OK:      true,
		}); err != nil {
			t.Fatalf("gc 期间第 %d 条写入失败: %v", i, err)
		}
	}
	wg.Wait()
	if err := <-gcErr; err != nil {
		t.Fatalf("gc 失败: %v", err)
	}

	// gc 结束后必须能把攒下的记录提交上去。
	if err := log.Flush(); err != nil {
		t.Fatalf("gc 后提交失败: %v", err)
	}
	if got := log.Dropped(); got != 0 {
		t.Fatalf("出现 %d 条审计被丢弃", got)
	}

	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(items) != base+during {
		t.Fatalf("记录应 %d 条，实际 %d 条：gc 期间写入的内容丢了", base+during, len(items))
	}
	// 抽查最后一条，确认内容连贯而不只是条数对上。
	last := items[len(items)-1]
	if last.Subject != fmt.Sprintf("during-%d", during-1) {
		t.Fatalf("最后一条不是 gc 期间写的：%q", last.Subject)
	}
}

// TestFlushDuringGcDoesNotFail 验证 gc 期间调用 Flush 不报错。
//
// 冷备推送前会周期性 Flush。gc 期间它应该安静地什么都不做
// （记录已在工作树里，等 gc 结束再提交），而不是把
// "object not found" 当成审计冲写失败报上去。
func TestFlushDuringGcDoesNotFail(t *testing.T) {
	log, at := writeForGc(t, 12)

	done := make(chan error, 1)
	go func() { done <- log.gcAt(1) }()

	// 与 gc 并发地反复 Flush。
	for i := 0; i < 20; i++ {
		if err := log.Flush(); err != nil {
			t.Fatalf("gc 期间 Flush 报错: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	if err := <-done; err != nil {
		t.Fatalf("gc 失败: %v", err)
	}
	if err := log.Append(Entry{At: at.Add(time.Hour), Event: EventAssetWrite, OK: true}); err != nil {
		t.Fatalf("追加失败: %v", err)
	}
	if err := log.Flush(); err != nil {
		t.Fatalf("gc 后提交失败: %v", err)
	}
	if got := log.Dropped(); got != 0 {
		t.Fatalf("出现 %d 条丢弃", got)
	}
}
