package secrets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

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
	if err != nil || cfg == nil || strings.TrimSpace(cfg.ServerURL) == "" {
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
	// OnStatus 报告解锁进度（程序化 / web unlock 分支切换等）。
	OnStatus func(message string)
	// OnUnlockURL 在本地 HTTP 服务就绪后回调，供 TUI 展示手动打开链接。
	OnUnlockURL func(url string)
	// OnBrowserError 在自动打开浏览器失败时回调。
	OnBrowserError func(err error)
	// UnlockTimeout 覆盖 web unlock 等待时长；零值时使用 unlock.WebUnlockTimeout()。
	UnlockTimeout time.Duration
}

// EnsureSession 若进程内无 session 则尝试程序化解锁，必要时触发 web unlock。
// 开发/agent 场景可设置环境变量 DEC_BW_PASSWORD（勿写入代码或配置）：
// 配合 ~/.dec/secrets/device.json 中的 two_factor_remember 令牌，可在新进程内
// 免 web unlock、免 2FA 建立 session。
// 已设置 DEC_BW_PASSWORD 且程序化登录失败时不会回退 web unlock（避免 agent 无限等待）。
func EnsureSession(ctx context.Context, opts *EnsureSessionOpts) error {
	onStatus := authStatusFunc(opts)

	hasSession := HasSession()
	authStatus(onStatus, "session check: hasSession=%v", hasSession)
	if hasSession {
		return nil
	}

	configured, err := IsConfigured()
	if err != nil {
		return fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		return fmt.Errorf("Bitwarden 未配置")
	}

	unlocked, passwordSet, err := tryProgrammaticUnlock(ctx, onStatus)
	if err != nil {
		return err
	}
	if unlocked {
		authStatus(onStatus, "session ready")
		return nil
	}
	if passwordSet {
		return fmt.Errorf("程序化登录未成功，且已设置 DEC_BW_PASSWORD，不会启动 web unlock")
	}

	timeout := unlock.WebUnlockTimeout()
	if opts != nil && opts.UnlockTimeout > 0 {
		timeout = opts.UnlockTimeout
	}
	authStatus(onStatus, "web unlock: starting (timeout=%s)", timeout)

	unlockCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	unlockOpts := unlock.Options{
		Authenticator: authenticatorFactory(),
		InitialEmail:  KnownEmail(),
		OnSession:     SetSession,
		OnEmailSaved:  SaveEmail,
		OnStatus:      onStatus,
	}
	if opts != nil {
		unlockOpts.OnReady = opts.OnUnlockURL
		unlockOpts.OnBrowserError = opts.OnBrowserError
	}

	err = unlockRun(unlockCtx, unlockOpts)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			authStatus(onStatus, "web unlock: timeout - no user input")
			return fmt.Errorf("web unlock 超时（%s）: 未收到用户输入", timeout)
		}
		return err
	}
	authStatus(onStatus, "web unlock: success")
	authStatus(onStatus, "session ready")
	return nil
}

func authStatusFunc(opts *EnsureSessionOpts) func(string) {
	if opts == nil || opts.OnStatus == nil {
		return nil
	}
	return opts.OnStatus
}

func authStatus(onStatus func(string), format string, args ...any) {
	if onStatus == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if !strings.HasPrefix(msg, "[auth]") {
		msg = "[auth] " + msg
	}
	onStatus(msg)
}

// tryProgrammaticUnlock 尝试用 DEC_BW_PASSWORD 程序化解锁。
// 集成 / live 测试通过 ApplyIntegrationAuth 从 `.secrets/dec/integration/bitwarden.yaml`
// 把密码注入进程环境，这里只读环境变量，保持单测不依赖仓库内是否存在凭据文件。
// passwordSet=true 表示已提供密码；此情况下失败时不应回退 web unlock。
func tryProgrammaticUnlock(ctx context.Context, onStatus func(string)) (unlocked bool, passwordSet bool, err error) {
	password := strings.TrimSpace(os.Getenv("DEC_BW_PASSWORD"))
	if password == "" {
		authStatus(onStatus, "programmatic unlock: skipped (DEC_BW_PASSWORD not set)")
		return false, false, nil
	}
	passwordSet = true

	if err := ensureIntegrationEmailConfigured(); err != nil {
		authStatus(onStatus, "programmatic unlock: failed: sync email: %v", err)
		return false, true, fmt.Errorf("同步 Bitwarden 邮箱失败: %w", err)
	}

	email := KnownEmail()
	if email == "" {
		authStatus(onStatus, "programmatic unlock: failed: Bitwarden email not configured")
		return false, true, fmt.Errorf("已设置 DEC_BW_PASSWORD 但未配置 Bitwarden 邮箱")
	}

	deviceID, devErr := EnsureDeviceIdentifier()
	if devErr != nil {
		authStatus(onStatus, "programmatic unlock: failed: device ID: %v", devErr)
		return false, true, fmt.Errorf("读取设备标识失败: %w", devErr)
	}
	authStatus(onStatus, "programmatic unlock: attempting (email=%s, deviceID=%s)", email, deviceID)

	auth := authenticatorFactory()
	token, need2FA, unlockErr := auth.Unlock(ctx, email, password)
	if unlockErr != nil {
		authStatus(onStatus, "programmatic unlock: failed: %v", unlockErr)
		return false, true, fmt.Errorf("Bitwarden 程序化登录失败: %w", unlockErr)
	}
	if need2FA {
		authStatus(onStatus, "programmatic unlock: failed: 2FA required (DEC_BW_PASSWORD set, web unlock disabled)")
		return false, true, fmt.Errorf("程序化登录仍需 2FA；已设置 DEC_BW_PASSWORD，不会启动 web unlock")
	}

	SetSession(token)
	authStatus(onStatus, "programmatic unlock: success")
	return true, true, nil
}
