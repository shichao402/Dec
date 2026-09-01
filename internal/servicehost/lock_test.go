package servicehost

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/unlock"
	"github.com/shichao402/Dec/internal/service"
	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNonLoopbackListenRequiresTLS(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	root := os.Getenv("DEC_HOME")
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("kind: global\nversion: 1\nmanagement_listen: 0.0.0.0:8443\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadListenSettings(); err == nil {
		t.Fatal("非 loopback 且无 TLS 应拒绝启动")
	}
}

func TestLoopbackListenDefault(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	settings, err := loadListenSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Addr != defaultListenAddr {
		t.Fatalf("addr = %q", settings.Addr)
	}
	if len(settings.Opts) != 0 {
		t.Fatal("默认不应启用 TLS")
	}
}

func TestLockedRejectsInvoke(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	secrets.ClearSession()
	t.Cleanup(secrets.ClearSession)

	ctx := startTestServer(t)

	api, err := connectRaw(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()

	_, err = api.RPC().Shutdown(ctx, &servicev1.ShutdownRequest{Reason: "probe"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("锁定时应拒绝 Shutdown: %v", err)
	}

	pong, err := api.RPC().Ping(ctx, &servicev1.PingRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if pong.GetUnlocked() {
		t.Fatal("锁定时 Ping.unlocked 应为 false")
	}
}

func TestAuthenticateUnlocksInstance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DEC_HOME", home)
	secrets.ClearSession()
	t.Cleanup(secrets.ClearSession)
	if err := secrets.SaveConfig(&secrets.Config{ServerURL: secrets.DefaultServerURL, Email: "alice@dec.test"}); err != nil {
		t.Fatal(err)
	}
	secrets.SetAuthenticatorForTest(unlock.NewStubAuthenticator("pw", "", "sess"))
	t.Cleanup(secrets.ResetAuthenticatorForTest)

	ctx := startTestServer(t)

	api, err := connectRaw(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()

	resp, err := api.RPC().Authenticate(ctx, &servicev1.AuthenticateRequest{Email: "alice@dec.test", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetError() != "" || !resp.GetUnlocked() || resp.GetControlToken() == "" {
		t.Fatalf("Authenticate = %#v", resp)
	}
	if _, err := api.RPC().GetActiveOperation(ctx, &servicev1.GetActiveOperationRequest{}); err != nil {
		t.Fatalf("解锁后应用 listen token 应能调用业务 RPC: %v", err)
	}
	pong, err := api.RPC().Ping(ctx, &servicev1.PingRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !pong.GetUnlocked() {
		t.Fatal("解锁后 Ping.unlocked 应为 true")
	}
}

// startTestServer 起一个服务并保证用例结束前它已完全退出。
//
// 必须等退出：Run 的收尾会删发现文件与释放 server.lock，而这两处路径都来自
// DEC_HOME。若放任 goroutine 跨用例收尾，它会删掉下一个用例刚写的 server.json。
// t.Cleanup 是 LIFO，本函数在 t.Setenv 之后调用，因此退出时 DEC_HOME 仍是本用例的值。
func startTestServer(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Run(ctx, "test")
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("dec-server 未在超时内退出")
		}
	})
	waitMetadata(t)
	return ctx
}

func waitMetadata(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := service.ReadMetadata(); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("dec-server 未写出发现文件")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func connectRaw(ctx context.Context) (*service.Client, error) {
	return service.Connect(ctx, "test", "lock-test", "test")
}
