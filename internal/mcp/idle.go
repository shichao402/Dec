package mcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"
)

// stdinIdleTimeout 是无 stdin 字节后自行退出的时限。
//
// Cursor Agent ACP（cursor-agent … index.js acp）会为每次会话拉起一对
// node.exe + dec-mcp，会话结束后两边都不退出。父进程看门狗在这种情况下
// 帮不上忙：父进程还活着。stdin 长期无数据是「会话已死、管道还挂着」的
// 可观测信号。时限必须长过一次正常 tool 调用间隔，短过「堆几天」。
var stdinIdleTimeout = 30 * time.Minute

type idleReader struct {
	r    io.Reader
	last atomic.Int64
}

func newIdleReader(r io.Reader) *idleReader {
	ir := &idleReader{r: r}
	ir.last.Store(time.Now().UnixNano())
	return ir
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.last.Store(time.Now().UnixNano())
	}
	return n, err
}

func (r *idleReader) Close() error { return nil }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func watchStdinIdle(parent context.Context, r *idleReader, idle time.Duration) (context.Context, func()) {
	if idle <= 0 || r == nil {
		return parent, func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	stop := make(chan struct{})
	interval := idle / 2
	switch {
	case interval > 5*time.Second:
		interval = 5 * time.Second
	case interval < 10*time.Millisecond:
		interval = 10 * time.Millisecond
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				last := time.Unix(0, r.last.Load())
				if time.Since(last) < idle {
					continue
				}
				fmt.Fprintf(os.Stderr, "[dec:mcp] stdin 已空闲 %s，退出以免宿主不回收会话时堆积进程 pid=%d\n",
					idle, os.Getpid())
				cancel()
				return
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
		cancel()
	}
}
