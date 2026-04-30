package freshness

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// CheckResult 描述一次陈旧度检查的结果。
type CheckResult struct {
	// Skipped 为 true 表示这次调用被节流或环境变量禁用，未触发网络 fetch。
	Skipped bool
	// LocalCommit 来自 .dec/.version，可能为空（项目还没 pull）。
	LocalCommit string
	// RemoteCommit 来自 git fetch 后 bare repo 的默认分支。
	RemoteCommit string
	// Stale 为 true 表示 LocalCommit != RemoteCommit 且两者都非空。
	Stale bool
	// Err 记录首个阻断性错误，但调用方通常应把它当作“沉默略过”信号。
	Err error
}

// EnvDisable 等效于 DEC_FRESHNESS_CHECK=off。
const EnvDisable = "DEC_FRESHNESS_CHECK"

// EnvInterval 覆盖节流窗口，格式遵循 time.ParseDuration，例如 "24h"、"30m"。
const EnvInterval = "DEC_FRESHNESS_INTERVAL"

// IsDisabled 判断是否通过环境变量禁用检查。
func IsDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvDisable)))
	switch v {
	case "off", "0", "false", "no":
		return true
	}
	return false
}

// Interval 读取节流窗口，解析失败回退到 DefaultInterval。
func Interval() time.Duration {
	v := strings.TrimSpace(os.Getenv(EnvInterval))
	if v == "" {
		return DefaultInterval
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return DefaultInterval
	}
	return d
}

// Check 是 cmd 层的便利入口：读本地版本 → 节流判断 → 远端 fetch → 对比。
//
// 它内部不会 panic，不会触发 os.Exit。所有错误都被记录在 CheckResult.Err 里，
// 调用方应根据 result.Stale 决定是否打印提示。
//
// ctx 用来给整次 fetch 加超时；超时到就立刻返回 Skipped=true。
//
// Deprecated: 同步实现会阻塞主命令 ~1-3s（真实 git fetch 耗时）。
// 新代码应使用 StartBackgroundCheck + EmitCachedHint 异步路径。
// 本函数保留给老调用方和测试，不再由 cmd 层调用。
func Check(ctx context.Context, projectRoot string) CheckResult {
	if IsDisabled() {
		return CheckResult{Skipped: true}
	}

	meta, err := LoadVersionMeta(projectRoot)
	if err != nil || meta == nil || meta.Commit == "" {
		// 项目没有 .dec/.version 或读不出来，不属于“过时”的范畴。
		return CheckResult{Skipped: true, Err: err}
	}

	stateFile, err := StateFilePath(projectRoot)
	if err != nil {
		return CheckResult{Skipped: true, Err: err}
	}

	if !ShouldCheck(stateFile, Interval()) {
		return CheckResult{Skipped: true, LocalCommit: meta.Commit}
	}

	// fetch 放到独立 goroutine，让 ctx 能真正兜底超时。
	type fetchResult struct {
		hash string
		err  error
	}
	ch := make(chan fetchResult, 1)
	go func() {
		h, e := FetchRemoteHead()
		ch <- fetchResult{h, e}
	}()

	select {
	case <-ctx.Done():
		return CheckResult{Skipped: true, LocalCommit: meta.Commit, Err: ctx.Err()}
	case r := <-ch:
		// 无论成功失败，都更新节流戳，避免反复尝试失败的 fetch。
		_ = RecordCheck(stateFile)
		if r.err != nil {
			return CheckResult{LocalCommit: meta.Commit, Err: r.err}
		}
		stale := r.hash != "" && r.hash != meta.Commit
		return CheckResult{
			LocalCommit:  meta.Commit,
			RemoteCommit: r.hash,
			Stale:        stale,
		}
	}
}

// FormatHint 产出一行给用户看的提示文案。
func FormatHint(result CheckResult) string {
	if !result.Stale {
		return ""
	}
	return fmt.Sprintf("💡 当前项目的 Dec 资产已落后远端（本地 %s，远端 %s）。执行 `dec pull` 可更新。",
		ShortHash(result.LocalCommit), ShortHash(result.RemoteCommit))
}

// WriteHint 若检测到过时则把提示写入 w。w 通常是 stderr。
func WriteHint(w io.Writer, result CheckResult) {
	if w == nil {
		return
	}
	hint := FormatHint(result)
	if hint == "" {
		return
	}
	fmt.Fprintln(w, hint)
}
