//go:build live

package secrets

import (
	"context"
	"os"
	"testing"
)

// programmaticUnlock 使用 ~/.dec/secrets/device.json 中的持久化 deviceIdentifier
// 与 two_factor_remember 令牌登录；勿传入自定义 deviceID，否则 remember token 失效且可能被清除。
func programmaticUnlock(t *testing.T, cfg *Config, password string) (token string, need2FA bool, err error) {
	t.Helper()
	auth, err := NewBWAuthenticator(cfg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	email := KnownEmail()
	if email == "" {
		email = cfg.Email
	}
	token, need2FA, err = auth.Unlock(context.Background(), email, password)
	if err != nil {
		t.Fatalf("Unlock() = %v", err)
	}
	if !need2FA {
		SetSession(token)
		t.Cleanup(ClearSession)
	}
	return token, need2FA, nil
}

// 运行: DEC_BW_PASSWORD='...' go test -tags live ./pkg/secrets -run TestLive_ProgrammaticUnlock -v
func TestLive_ProgrammaticUnlock(t *testing.T) {
	password := os.Getenv("DEC_BW_PASSWORD")
	if password == "" {
		t.Skip("设置 DEC_BW_PASSWORD 以运行 live 登录测试")
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	token, need2FA, err := programmaticUnlock(t, cfg, password)
	if need2FA {
		t.Fatal("程序化登录仍需 2FA：检查 device.json 中 remember token 与 identifier 是否匹配")
	}
	if token == "" {
		t.Fatal("未返回 access token")
	}
	client, err := NewAPIClient(cfg, token, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.PullBundle(context.Background(), PullBundleRequest{
		DecBundleName: "vikunja",
		Binding:       BundleBinding{DecBundleName: "vikunja", SecretsBundleName: "vikunja_workflow"},
	})
	if err != nil {
		t.Fatalf("PullBundle() = %v", err)
	}
	t.Log("程序化登录成功，PullBundle 可用")
}

// 运行: DEC_BW_PASSWORD='...' go test -tags live ./pkg/secrets -run TestLive_PushBundle -v
func TestLive_PushBundle(t *testing.T) {
	password := os.Getenv("DEC_BW_PASSWORD")
	if password == "" {
		t.Skip("设置 DEC_BW_PASSWORD 以运行 live push 测试")
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	token, need2FA, err := programmaticUnlock(t, cfg, password)
	if need2FA {
		t.Fatal("remember token 应跳过 2FA；若失败请检查 device.json identifier 是否与 Bitwarden 记住的设备一致")
	}

	client, err := NewAPIClient(cfg, token, nil)
	if err != nil {
		t.Fatal(err)
	}

	projectRoot := os.Getenv("DEC_PROJECT_ROOT")
	if projectRoot == "" {
		projectRoot = "/Users/firo/workspace/Dec"
	}
	result, err := PushBundle(context.Background(), client, PushBundleRequest{
		ProjectRoot:   projectRoot,
		DecBundleName: "vikunja",
		Binding:       BundleBinding{DecBundleName: "vikunja", SecretsBundleName: "vikunja_workflow"},
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	t.Logf("PushBundle: created=%d updated=%d paths=%v", result.Created, result.Updated, result.Paths)
}

// 运行: DEC_BW_PASSWORD='...' go test -tags live ./pkg/secrets -run TestLive_PushThenPull -v
func TestLive_PushThenPull(t *testing.T) {
	password := os.Getenv("DEC_BW_PASSWORD")
	if password == "" {
		t.Skip("设置 DEC_BW_PASSWORD 以运行 live push/pull 往返测试")
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	token, need2FA, err := programmaticUnlock(t, cfg, password)
	if need2FA {
		t.Fatal("remember token 应跳过 2FA；若失败请检查 device.json identifier 是否与 Bitwarden 记住的设备一致")
	}

	client, err := NewAPIClient(cfg, token, nil)
	if err != nil {
		t.Fatal(err)
	}

	projectRoot := os.Getenv("DEC_PROJECT_ROOT")
	if projectRoot == "" {
		projectRoot = "/Users/firo/workspace/Dec"
	}
	req := PushBundleRequest{
		ProjectRoot:   projectRoot,
		DecBundleName: "vikunja",
		Binding:       BundleBinding{DecBundleName: "vikunja", SecretsBundleName: "vikunja_workflow"},
	}
	pushResult, err := PushBundle(context.Background(), client, req)
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	t.Logf("PushBundle: created=%d updated=%d paths=%v", pushResult.Created, pushResult.Updated, pushResult.Paths)

	pullReq := PullBundleRequest{
		ProjectRoot:   projectRoot,
		DecBundleName: "vikunja",
		Binding:       BundleBinding{DecBundleName: "vikunja", SecretsBundleName: "vikunja_workflow"},
	}
	paths, err := PullBundle(context.Background(), client, pullReq)
	if err != nil {
		t.Fatalf("PullBundle() after push = %v", err)
	}
	t.Logf("PullBundle: paths=%v", paths)
}
