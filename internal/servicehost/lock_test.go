package servicehost

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/service"
	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
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

func TestLockedAllowsLocalShutdownForRuntimeUpgrade(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	secrets.ClearSession()
	t.Cleanup(secrets.ClearSession)

	ctx := startTestServer(t)

	api, err := connectRaw(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()

	pong, err := api.RPC().Ping(ctx, &servicev1.PingRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if pong.GetUnlocked() {
		t.Fatal("锁定时 Ping.unlocked 应为 false")
	}

	resp, err := api.RPC().Shutdown(ctx, &servicev1.ShutdownRequest{Reason: "runtime-upgrade"})
	if err != nil || !resp.GetAccepted() {
		t.Fatalf("持有本机 listen token 时锁定态应允许 Shutdown: resp=%v err=%v", resp, err)
	}
}

func TestOlderClientCannotControlNewerServer(t *testing.T) {
	server := &Server{version: "v2.0.0"}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		service.ClientVersionHeader, "v1.9.9",
	))
	err := server.authorizeRPC(ctx, servicev1.DecService_Authenticate_FullMethodName)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("低版本门面应在 Authenticate 前被服务拒绝: %v", err)
	}
	if err := server.authorizeRPC(ctx, servicev1.DecService_Ping_FullMethodName); err != nil {
		t.Fatalf("Ping 必须保持可用以读取服务版本: %v", err)
	}
}

func TestInteractiveUnlockRequiresLocalMCPListenToken(t *testing.T) {
	server := &Server{listenToken: "local-token"}
	localMCP := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		service.TokenHeader, "local-token",
		service.InteractiveAuthHeader, "1",
	))
	if !server.allowsInteractiveUnlock(localMCP, "mcp") {
		t.Fatal("local MCP with listen token should be interactive")
	}
	if server.allowsInteractiveUnlock(localMCP, "console") {
		t.Fatal("Console must use Authenticate directly")
	}
	remoteMCP := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		service.TokenHeader, "control-token",
		service.InteractiveAuthHeader, "1",
	))
	if server.allowsInteractiveUnlock(remoteMCP, "mcp") {
		t.Fatal("remote/control-token MCP must not launch a desktop app")
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
	secrets.SetAuthenticatorForTest(secrets.NewStubAuthenticator("pw", "", "sess"))
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
