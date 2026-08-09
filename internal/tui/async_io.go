package tui

import "context"

// Shell 异步 IO 约定（Delete / refresh / secrets 等跨页加载均应遵循）：
//
//  1. 任务跟 Shell 生命周期走，不跟当前页面走——切页禁止 cancel。
//  2. 用独立 busy 信号（asyncLoad / asyncBatch + ioBusyLabel）表达「在飞」。
//  3. 防重复：已在飞且请求未升级则 no-op；缓存仍有效且未 force 则 no-op。
//  4. 仅在「被更新一代请求取代」或用户主动取消 / 进程退出时 abort；用 gen 丢弃过期结果。
//  5. 远端 / 昂贵 IO 更要遵循以上规则；切页打断等于白白烧掉一次 round-trip。
//
// 落地：单次任务用 asyncLoad；refresh 这类并联多段用 asyncBatch。

// asyncLoad 跟踪一类可跨页飞行的 Shell IO。
type asyncLoad struct {
	loading bool
	gen     uint64
	cancel  context.CancelFunc
}

func (a *asyncLoad) busy() bool {
	return a != nil && a.loading
}

// begin 取消上一代（若有），开启新一代并返回 ctx/gen 交给 tea.Cmd。
func (a *asyncLoad) begin() (context.Context, uint64) {
	a.abort()
	a.gen++
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.loading = true
	return ctx, a.gen
}

// beginGen 同 begin，但调用方不需要 ctx（纯 tea.Cmd 无取消的短任务）。
func (a *asyncLoad) beginGen() uint64 {
	_, gen := a.begin()
	return gen
}

// abort 仅用于被新一代取代或用户显式取消；切页不得调用。
func (a *asyncLoad) abort() {
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
}

// clear 强制结束当前代（用户取消）；推进 gen，使在飞结果过期丢弃。
func (a *asyncLoad) clear() {
	a.abort()
	a.loading = false
	a.gen++
}

// finish 若 gen 匹配当前代则清 busy 并返回 true；过期结果返回 false（调用方丢弃）。
func (a *asyncLoad) finish(gen uint64) bool {
	if gen != a.gen {
		return false
	}
	a.cancel = nil
	a.loading = false
	return true
}

// asyncBatch 跟踪一次并联多段的 shell 刷新（如 overview+assets+settings+…）。
type asyncBatch struct {
	asyncLoad
	pending int
}

func (b *asyncBatch) busy() bool {
	return b != nil && b.loading
}

// beginParts 开启一代并期望 n 个分片回齐。会取代上一代。
func (b *asyncBatch) beginParts(n int) uint64 {
	if n < 0 {
		n = 0
	}
	b.abort()
	b.gen++
	b.loading = true
	b.pending = n
	b.cancel = nil // batch 分片 cmd 目前不共用单一 ctx
	return b.gen
}

// acceptPart 校验 gen；通过后减少 pending。返回 false 表示过期应丢弃整条消息。
// pending 归零时清 busy。单片仍须由调用方应用数据。
func (b *asyncBatch) acceptPart(gen uint64) bool {
	if gen != b.gen {
		return false
	}
	if b.pending > 0 {
		b.pending--
	}
	if b.pending == 0 {
		b.loading = false
	}
	return true
}
