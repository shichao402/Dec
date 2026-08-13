package unlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// DefaultUnlockPort 为 web unlock 本地 HTTP 服务的首选固定端口（ephemeral 范围高位）。
const DefaultUnlockPort = 59123

// DefaultWebUnlockTimeout 为 web unlock 等待用户输入的最长时间。
const DefaultWebUnlockTimeout = 5 * time.Minute

// WebUnlockTimeout 返回 web unlock 超时；可通过 DEC_BW_UNLOCK_TIMEOUT 覆盖（如 "5m"、"300s"）。
func WebUnlockTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("DEC_BW_UNLOCK_TIMEOUT"))
	if raw == "" {
		return DefaultWebUnlockTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return DefaultWebUnlockTimeout
	}
	return d
}

// Options 配置 web unlock 流程。
type Options struct {
	Authenticator Authenticator
	OpenBrowser   BrowserOpener
	ListenAddr    string
	InitialEmail  string
	OnEmailSaved  func(email string) error
	OnSession     func(session string)
	// OnStatus 报告 web unlock 进度（带 [auth] 前缀由调用方或本包补齐）。
	OnStatus func(message string)
	// OnReady 在 HTTP 服务就绪后、尝试打开浏览器前回调（可用于展示手动打开链接）。
	OnReady func(url string)
	// OnBrowserError 在自动打开浏览器失败时回调；流程仍继续等待用户手动访问。
	OnBrowserError func(err error)
}

// Run 启动本地 HTTP 解锁服务并阻塞至成功、失败或 ctx 取消/超时。
func Run(ctx context.Context, opts Options) error {
	if opts.Authenticator == nil {
		return fmt.Errorf("unlock: 缺少 Authenticator")
	}
	status := unlockStatusFunc(opts.OnStatus)

	// 未显式注入 opener 意味着会打开真实浏览器并等待人工输入，
	// 测试环境下直接拒绝，避免弹窗打断无人值守的测试。
	opener := opts.OpenBrowser
	if opener == nil {
		if !WebUnlockAllowed() {
			return ErrWebUnlockBlocked
		}
		opener = defaultBrowserOpener
	}

	onSession := opts.OnSession
	srv := newServer(opts.Authenticator, opts.InitialEmail, func(session string) {
		if onSession != nil {
			onSession(session)
		}
	}, opts.OnEmailSaved)

	baseURL, err := srv.listenAndServe(ctx, opts.ListenAddr)
	if err != nil {
		return err
	}
	unlockStatus(status, "web unlock: starting server on %s", baseURL)

	if err := srv.waitReady(ctx, baseURL); err != nil {
		srv.shutdown()
		return err
	}
	unlockStatus(status, "web unlock: server ready")

	if opts.OnReady != nil {
		opts.OnReady(baseURL)
	}
	if err := opener(baseURL); err != nil {
		if opts.OnBrowserError != nil {
			opts.OnBrowserError(err)
		}
		unlockStatus(status, "web unlock: browser open failed: %v", err)
	} else {
		unlockStatus(status, "web unlock: browser opened")
	}

	timeout := remainingTimeout(ctx)
	if timeout > 0 {
		unlockStatus(status, "web unlock: waiting for callback (timeout=%s)", timeout.Round(time.Second))
	} else {
		unlockStatus(status, "web unlock: waiting for callback")
	}

	select {
	case <-ctx.Done():
		srv.shutdown()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			unlockStatus(status, "web unlock: timeout - no user input")
		}
		return ctx.Err()
	case err := <-srv.done:
		if err != nil {
			return err
		}
		return nil
	}
}

func unlockStatusFunc(onStatus func(string)) func(string, ...any) {
	if onStatus == nil {
		return func(string, ...any) {}
	}
	return func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if !strings.HasPrefix(msg, "[auth]") {
			msg = "[auth] " + msg
		}
		onStatus(msg)
	}
}

func unlockStatus(status func(string, ...any), format string, args ...any) {
	status(format, args...)
}

func remainingTimeout(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		return time.Until(deadline)
	}
	return 0
}
