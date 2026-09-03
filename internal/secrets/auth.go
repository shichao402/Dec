package secrets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/consoleopen"
)

// authenticatorFactory 供测试注入 mock Authenticator。
var authenticatorFactory = defaultAuthenticator

// httpClientFactory 供测试注入 HTTP 客户端。
var httpClientFactory = func() *http.Client {
	return http.DefaultClient
}

var (
	openConsoleUnlock = consoleopen.OpenUnlockLocal
	consoleAvailable  = consoleopen.Available
)

func defaultAuthenticator() Authenticator {
	cfg, err := LoadConfig()
	if err != nil || cfg == nil || strings.TrimSpace(cfg.ServerURL) == "" {
		return NewStubAuthenticator("", "", "")
	}
	auth, err := NewBWAuthenticator(cfg, "", httpClientFactory())
	if err != nil {
		return NewStubAuthenticator("", "", "")
	}
	return auth
}

const DefaultConsoleUnlockTimeout = 5 * time.Minute

var (
	ErrConsoleUnlockRequired    = errors.New("CONSOLE_UNLOCK_REQUIRED: 需要在 Dec Console 中解锁 Bitwarden")
	ErrConsoleUnlockUnavailable = errors.New("CONSOLE_UNLOCK_UNAVAILABLE: 当前环境不能自动打开 Dec Console")
	ErrConsoleUnlockTimeout     = errors.New("CONSOLE_UNLOCK_TIMEOUT: 等待 Dec Console 解锁超时")
	ErrConsoleUnlockCanceled    = errors.New("CONSOLE_UNLOCK_CANCELED: 等待 Dec Console 解锁已取消")
	ErrConsoleLaunchFailed      = errors.New("CONSOLE_LAUNCH_FAILED: 无法打开 Dec Console")
)

type ensureSessionOptsKey struct{}

// EnsureSessionOpts describes the request that needs a Bitwarden session.
type EnsureSessionOpts struct {
	RequestSource  string
	Facade         string
	ClientID       string
	Operation      string
	OperationID    string
	ProjectRoot    string
	WorkspacePlane string
	// InteractiveLocal permits opening the local Console. Remote/headless/CI
	// callers must leave it false and receive ErrConsoleUnlockRequired.
	InteractiveLocal bool
	OnStatus         func(message string)
	UnlockTimeout    time.Duration
}

func WithEnsureSessionOpts(ctx context.Context, opts EnsureSessionOpts) context.Context {
	return context.WithValue(ctx, ensureSessionOptsKey{}, opts)
}

func ensureSessionOptsFromContext(ctx context.Context) *EnsureSessionOpts {
	if opts, ok := ctx.Value(ensureSessionOptsKey{}).(EnsureSessionOpts); ok {
		return &opts
	}
	return nil
}

// EnsureSession reuses an in-memory session, attempts DEC_BW_PASSWORD, or
// opens the local Console and waits for Authenticate to complete.
// 开发/agent 场景可设置环境变量 DEC_BW_PASSWORD（勿写入代码或配置）：
// 配合 ~/.dec/secrets/device.json 中的 two_factor_remember 令牌，可在新进程内
// 免 Console、免 2FA 建立 session。
func EnsureSession(ctx context.Context, opts *EnsureSessionOpts) error {
	if opts == nil {
		opts = ensureSessionOptsFromContext(ctx)
	}
	onStatus := authStatusFunc(opts)

	if InstanceUnlocked() {
		authStatus(onStatus, "session check: ready")
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
		return fmt.Errorf("程序化登录未成功，且已设置 DEC_BW_PASSWORD，不会启动 Console")
	}

	if opts == nil || !opts.InteractiveLocal || !consoleAvailable() {
		return fmt.Errorf("%w: %w；请在有桌面的机器上打开 Console 完成认证",
			ErrConsoleUnlockRequired, ErrConsoleUnlockUnavailable)
	}

	timeout := DefaultConsoleUnlockTimeout
	if opts != nil && opts.UnlockTimeout > 0 {
		timeout = opts.UnlockTimeout
	}
	authStatus(onStatus, "console unlock: requesting (timeout=%s)", timeout)
	if err := requestConsoleUnlock(); err != nil {
		return fmt.Errorf("%w: %w: %v", ErrConsoleUnlockRequired, ErrConsoleLaunchFailed, err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := WaitForInstanceUnlock(waitCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			authStatus(onStatus, "console unlock: timeout")
			return fmt.Errorf("%w: %w（%s）", ErrConsoleUnlockRequired, ErrConsoleUnlockTimeout, timeout)
		}
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("%w: %w: %w", ErrConsoleUnlockRequired, ErrConsoleUnlockCanceled, err)
		}
		return err
	}
	authStatus(onStatus, "console unlock: success")
	authStatus(onStatus, "session ready")
	return nil
}

var consoleLaunchState struct {
	sync.Mutex
	last time.Time
}

func requestConsoleUnlock() error {
	consoleLaunchState.Lock()
	defer consoleLaunchState.Unlock()
	if time.Since(consoleLaunchState.last) < 2*time.Second {
		return nil
	}
	if err := openConsoleUnlock(); err != nil {
		return err
	}
	consoleLaunchState.last = time.Now()
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
// passwordSet=true 表示已提供密码；此情况下失败时不应打开 Console。
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
		authStatus(onStatus, "programmatic unlock: failed: 2FA required")
		return false, true, fmt.Errorf("程序化登录仍需 2FA；已设置 DEC_BW_PASSWORD，不会启动 Console")
	}

	SetSession(token)
	if !InstanceUnlocked() {
		return false, true, fmt.Errorf("程序化登录未取得 vault key")
	}
	authStatus(onStatus, "programmatic unlock: success")
	return true, true, nil
}
