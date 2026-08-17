package mcp

import (
	"context"
	"os"
	"os/signal"
	"time"
)

const parentWatchInterval = 3 * time.Second

// withExitWatchers 在传入 ctx 之上叠加两个退出触发器：
//  1. 系统中断信号（Ctrl-C / SIGTERM 等）。
//  2. 父进程存活看门狗：当拉起本进程的父进程（IDE / Agent / dec-exec 链）消失时取消 ctx。
//
// dec-mcp 是薄门面：正常退出依赖 MCP SDK 读到 stdin EOF。但在部分平台（尤其 Windows）
// 父进程异常退出后 stdin 可能收不到 EOF，SDK 的 Run 不自行返回，进程会变孤儿常驻并
// 持续占用内存 / 拖住 dec-server 空闲退出。父进程看门狗是这一情形的兜底。
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
		ticker := time.NewTicker(parentWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !processAlive(ppid) {
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
