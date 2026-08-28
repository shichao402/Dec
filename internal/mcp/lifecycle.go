package mcp

import (
	"context"
	"os"
	"os/signal"
	"time"
)

const parentWatchInterval = 3 * time.Second

// withExitWatchers 在传入 ctx 之上叠加退出触发器：
//  1. 系统中断信号（Ctrl-C / SIGTERM 等）。
//  2. 父进程看门狗：父进程消失，或同一 PID 被后来的无关进程占用时取消 ctx。
//
// 这只覆盖「父进程真的没了」。Cursor Agent ACP 会留下活着的 node.exe 父进程
// 和不再读写的 stdin；那种堆积由 stdin 空闲超时处理（见 idle.go）。
func withExitWatchers(parent context.Context) (context.Context, func()) {
	ctx, cancelSignal := signal.NotifyContext(parent, exitSignals()...)
	ctx, cancelWatch := context.WithCancel(ctx)

	ppid := os.Getppid()
	stop := make(chan struct{})
	go func() {
		// PPID<=1 表示父进程已被 init/系统接管（早已退出），无从监视，放弃看门狗。
		if ppid <= 1 {
			return
		}
		ident, ok := processIdentity(ppid)
		if !ok {
			cancelWatch()
			return
		}
		ticker := time.NewTicker(parentWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				got, still := processIdentity(ppid)
				if !still || got != ident {
					cancelWatch()
					return
				}
			}
		}
	}()

	var once bool
	return ctx, func() {
		if once {
			return
		}
		once = true
		close(stop)
		cancelWatch()
		cancelSignal()
	}
}
