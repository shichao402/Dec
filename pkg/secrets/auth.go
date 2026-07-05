package secrets

import (
	"context"
	"fmt"
	"net/http"

	"github.com/shichao402/Dec/pkg/secrets/unlock"
)

// authenticatorFactory 供测试注入 mock Authenticator。
var authenticatorFactory = defaultAuthenticator

// httpClientFactory 供测试注入 HTTP 客户端。
var httpClientFactory = func() *http.Client {
	return http.DefaultClient
}

// unlockRun 供测试替换 web unlock 流程。
var unlockRun = func(ctx context.Context, opts unlock.Options) error {
	return unlock.Run(ctx, opts)
}

func defaultAuthenticator() unlock.Authenticator {
	cfg, err := LoadConfig()
	if err != nil || cfg == nil || !cfg.CanAuthenticate() {
		return unlock.NewStubAuthenticator("", "", "")
	}
	auth, err := NewBWAuthenticator(cfg, "", httpClientFactory())
	if err != nil {
		return unlock.NewStubAuthenticator("", "", "")
	}
	return auth
}

// EnsureSessionOpts 可选 unlock 回调。
type EnsureSessionOpts struct {
	// OnUnlockURL 在本地 HTTP 服务就绪后回调，供 TUI 展示手动打开链接。
	OnUnlockURL func(url string)
	// OnBrowserError 在自动打开浏览器失败时回调。
	OnBrowserError func(err error)
}

// EnsureSession 若进程内无 session 则阻塞触发 web unlock。
func EnsureSession(ctx context.Context, opts *EnsureSessionOpts) error {
	if HasSession() {
		return nil
	}
	configured, err := IsConfigured()
	if err != nil {
		return fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		return fmt.Errorf("Bitwarden 未配置")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !cfg.CanAuthenticate() {
		return fmt.Errorf("Bitwarden email 未配置")
	}
	unlockOpts := unlock.Options{
		Authenticator: authenticatorFactory(),
		OnSession:     SetSession,
	}
	if opts != nil {
		unlockOpts.OnReady = opts.OnUnlockURL
		unlockOpts.OnBrowserError = opts.OnBrowserError
	}
	return unlockRun(ctx, unlockOpts)
}
