package serviceapi

import (
	"context"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/service"
	"github.com/shichao402/Dec/internal/servicehost"
)

// startServer 在临时 DEC_HOME 里跑一个进程内 dec-server，并返回连好的门面。
func startServer(t *testing.T) *API {
	t.Helper()
	t.Setenv("DEC_HOME", t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- servicehost.Run(ctx, "test") }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("dec-server 未在超时内退出")
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := service.ReadMetadata(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dec-server 未写出服务发现文件")
		}
		time.Sleep(20 * time.Millisecond)
	}

	api, err := Connect(ctx, "tui", "test-client", "test")
	if err != nil {
		t.Fatalf("连接 dec-server 失败: %v", err)
	}
	t.Cleanup(func() { _ = api.Close() })
	SetDefault(api)
	t.Cleanup(func() { SetDefault(nil) })
	return api
}

// 服务端返回 null 时门面必须回 nil，否则调用方的 nil 判断会失效，
// 把「没有推断结果」当成「推断出一个字段全空的 project」。
func TestInferVaultProjectReturnsNilWhenServerHasNoResult(t *testing.T) {
	startServer(t)

	inference, err := InferVaultProject(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("InferVaultProject() 失败: %v", err)
	}
	if inference != nil {
		t.Fatalf("未连接仓库时应无推断结果，实际 = %#v", inference)
	}
}

func TestServerVersionAndShutdown(t *testing.T) {
	api := startServer(t)
	if api.ServerVersion() != "test" {
		t.Fatalf("ServerVersion = %q, 期望 test", api.ServerVersion())
	}
	if api.VersionMismatch() {
		t.Fatal("同为 test 时不应判定 mismatch")
	}

	if err := api.ShutdownServer(context.Background(), "test"); err != nil {
		t.Fatalf("ShutdownServer 失败: %v", err)
	}
	if _, err := service.ReadMetadata(); err == nil {
		t.Fatal("Shutdown 后服务发现文件应消失")
	}
}
