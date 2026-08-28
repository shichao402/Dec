package mcp

import (
	"context"
	"io"
	"os"
	"testing"
	"time"
)

func TestWatchStdinIdle_CancelsAfterSilence(t *testing.T) {
	r, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close(); _ = r.Close() })
	idle := newIdleReader(r)
	ctx, stop := watchStdinIdle(context.Background(), idle, 40*time.Millisecond)
	t.Cleanup(stop)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stdin 空闲后应取消 ctx")
	}
}

func TestWatchStdinIdle_ResetsOnRead(t *testing.T) {
	r, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close(); _ = r.Close() })
	idle := newIdleReader(r)
	ctx, stop := watchStdinIdle(context.Background(), idle, 80*time.Millisecond)
	t.Cleanup(stop)

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 8)
		for {
			if _, err := idle.Read(buf); err != nil {
				return
			}
		}
	}()
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			t.Fatal("有 stdin 流量时不应空闲退出")
		default:
		}
	}
	_ = w.Close()
	<-done
}

func TestIsUnexpandedPlaceholder(t *testing.T) {
	if !isUnexpandedPlaceholder("${workspaceFolder}") {
		t.Fatal("应识别未展开的 VS Code 变量")
	}
	if isUnexpandedPlaceholder(`D:\workspace\GitHub\Dec`) {
		t.Fatal("真实路径不应被当成占位符")
	}
}

func TestResolveProjectRoot_FallsBackToCwdForUnexpandedPlaceholder(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveProjectRoot("${workspaceFolder}"); got != cwd {
		t.Fatalf("got %q want cwd %q", got, cwd)
	}
}
