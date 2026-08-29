package secrets

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/shichao402/Dec/internal/secrets/unlock"
)

var (
	pendingAuthMu     sync.Mutex
	pendingAuth       unlock.Authenticator
	lockBypassForTest bool
)

// UnlockResult 是管理客户端 Authenticate 的结果；成功时 session 已写入进程内存。
type UnlockResult struct {
	Need2FA bool
}

// InstanceUnlocked 表示本实例已持有未过期的 Bitwarden session 与 vault key，
// 即管理控制权有效。
func InstanceUnlocked() bool {
	if lockBypassForTest {
		return true
	}
	return HasSession() && HasUserKey()
}

func UnlockForTest() {
	lockBypassForTest = true
	SetSession("test-session")
	SetUserKey(make([]byte, 64))
}

// UnlockWithPassword 用调用方传入的主密码（及可选 TOTP）解锁实例。
// 密码不落盘；2FA 中间态只留在进程内 Authenticator 上。
func UnlockWithPassword(ctx context.Context, email, password, totp string, rememberDevice bool) (*UnlockResult, error) {
	password = strings.TrimSpace(password)
	totp = strings.TrimSpace(totp)
	email = strings.TrimSpace(email)
	if totp != "" {
		return completePending2FA(ctx, totp, rememberDevice)
	}
	if password == "" {
		return nil, fmt.Errorf("主密码不能为空")
	}

	configured, err := IsConfigured()
	if err != nil {
		return nil, fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		return nil, fmt.Errorf("Bitwarden 未配置")
	}
	if email == "" {
		email = KnownEmail()
	}
	if email == "" {
		return nil, fmt.Errorf("未配置 Bitwarden 邮箱")
	}

	auth := authenticatorFactory()
	token, need2FA, err := auth.Unlock(ctx, email, password)
	if err != nil {
		clearPendingAuth()
		return nil, fmt.Errorf("Bitwarden 登录失败: %w", err)
	}
	if need2FA {
		pendingAuthMu.Lock()
		pendingAuth = auth
		pendingAuthMu.Unlock()
		return &UnlockResult{Need2FA: true}, nil
	}
	applyUnlockSession(token)
	return &UnlockResult{}, nil
}

func completePending2FA(ctx context.Context, totp string, rememberDevice bool) (*UnlockResult, error) {
	pendingAuthMu.Lock()
	auth := pendingAuth
	pendingAuthMu.Unlock()
	if auth == nil {
		return nil, fmt.Errorf("当前不需要二次验证")
	}
	token, err := auth.Verify2FA(ctx, totp, rememberDevice)
	if err != nil {
		return nil, fmt.Errorf("二次验证失败: %w", err)
	}
	applyUnlockSession(token)
	return &UnlockResult{}, nil
}

func applyUnlockSession(token string) {
	SetSession(token)
	if !HasUserKey() {
		SetUserKey(make([]byte, 64))
	}
	clearPendingAuth()
}

func ResetAuthenticatorForTest() {
	authenticatorFactory = defaultAuthenticator
	clearPendingAuth()
}

// SetAuthenticatorForTest 注入 Authenticator，仅测试使用。
func SetAuthenticatorForTest(auth unlock.Authenticator) {
	authenticatorFactory = func() unlock.Authenticator { return auth }
}

func clearPendingAuth() {
	pendingAuthMu.Lock()
	pendingAuth = nil
	pendingAuthMu.Unlock()
}
