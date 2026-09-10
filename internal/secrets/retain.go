package secrets

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/shichao402/Dec/internal/config"
)

// 保留的主密码只活在 dec-server 进程内存里：Console 勾选「保存主密码」时随
// Authenticate 送来，用于 session 到期或云端失效后静默重解锁，进程退出即消失。
// 禁止落盘、写日志或进入 operation 结果。
var (
	retainMu         sync.Mutex
	retainedEmail    string
	retainedPassword string
	retainedFailures int
	pending2FAEmail  string
	pending2FASecret string
)

// autoUnlockMu 串行化静默重解锁：并发请求同时发现锁定时只登录一次。
var autoUnlockMu sync.Mutex

// retainedFailureLimit 是连续静默解锁失败多少次后放弃保留凭据。
// 允许少量失败以容忍网络抖动，但远低于 Bitwarden 的账号锁定阈值。
const retainedFailureLimit = 3

// RetainUnlockPassword 保留主密码用于后续静默重解锁。password 为空表示不保留。
func RetainUnlockPassword(email, password string) {
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	retainMu.Lock()
	defer retainMu.Unlock()
	if password == "" {
		retainedEmail, retainedPassword, retainedFailures = "", "", 0
		return
	}
	retainedEmail, retainedPassword, retainedFailures = email, password, 0
}

// ClearRetainedPassword 丢弃保留的主密码，之后缺 session 必须重新人工解锁。
func ClearRetainedPassword() {
	retainMu.Lock()
	defer retainMu.Unlock()
	retainedEmail, retainedPassword, retainedFailures = "", "", 0
	pending2FAEmail, pending2FASecret = "", ""
}

// HasRetainedPassword 表示进程内是否留有可用于静默重解锁的主密码。
func HasRetainedPassword() bool {
	retainMu.Lock()
	defer retainMu.Unlock()
	return retainedPassword != ""
}

func retainedCredentials() (email, password string) {
	retainMu.Lock()
	defer retainMu.Unlock()
	return retainedEmail, retainedPassword
}

// retainPending2FA 记住 2FA 完成后要保留的主密码。
func retainPending2FA(email, password string) {
	retainMu.Lock()
	defer retainMu.Unlock()
	pending2FAEmail, pending2FASecret = strings.TrimSpace(email), strings.TrimSpace(password)
}

// applyPending2FARetention 在 2FA 完成后落实保留意图。
func applyPending2FARetention() {
	retainMu.Lock()
	email, password := pending2FAEmail, pending2FASecret
	pending2FAEmail, pending2FASecret = "", ""
	retainMu.Unlock()
	if password == "" {
		return
	}
	RetainUnlockPassword(email, password)
}

func noteRetainedFailure() {
	retainMu.Lock()
	defer retainMu.Unlock()
	if retainedPassword == "" {
		return
	}
	retainedFailures++
	if retainedFailures >= retainedFailureLimit {
		retainedEmail, retainedPassword, retainedFailures = "", "", 0
	}
}

func noteRetainedSuccess() {
	retainMu.Lock()
	defer retainMu.Unlock()
	retainedFailures = 0
}

// TryAutoUnlock 用进程内保留的主密码静默重解锁，不打开 Console、不等待人工输入。
// 没有保留凭据或登录失败时返回 false，由调用方回退到人工认证。
func TryAutoUnlock(ctx context.Context) bool {
	unlocked, _ := tryRetainedUnlock(ctx, nil)
	return unlocked
}

// AutoReunlockOnTimeout 返回解锁有效期到点后的策略。旧配置未包含开关时默认开启，
// 保持已保存主密码用户的无感续期行为。
func AutoReunlockOnTimeout() bool {
	cfg, err := config.LoadGlobalConfig()
	if err != nil || cfg == nil || cfg.AutoReunlockOnTimeout == nil {
		return true
	}
	return *cfg.AutoReunlockOnTimeout
}

func tryRetainedUnlock(ctx context.Context, onStatus func(string)) (bool, error) {
	email, password := retainedCredentials()
	if password == "" {
		return false, nil
	}
	if SessionTimedOut() && !AutoReunlockOnTimeout() {
		authStatus(onStatus, "auto unlock: skipped (disabled after session timeout)")
		return false, nil
	}
	autoUnlockMu.Lock()
	defer autoUnlockMu.Unlock()
	if InstanceUnlocked() {
		return true, nil
	}
	authStatus(onStatus, "auto unlock: attempting with retained password (email=%s)", email)

	auth := authenticatorFactory()
	token, need2FA, err := auth.Unlock(ctx, email, password)
	if err != nil {
		noteRetainedFailure()
		authStatus(onStatus, "auto unlock: failed: %v", err)
		return false, fmt.Errorf("用保留的主密码重新解锁失败: %w", err)
	}
	if need2FA {
		// remember token 失效后只能由人工补 TOTP，保留的密码单独无法完成。
		ClearRetainedPassword()
		authStatus(onStatus, "auto unlock: failed: 2FA required")
		return false, fmt.Errorf("用保留的主密码重新解锁仍需 2FA")
	}
	applyUnlockSession(token)
	if !InstanceUnlocked() {
		noteRetainedFailure()
		return false, fmt.Errorf("用保留的主密码重新解锁未取得 vault key")
	}
	noteRetainedSuccess()
	authStatus(onStatus, "auto unlock: success")
	return true, nil
}
