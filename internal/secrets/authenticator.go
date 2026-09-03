package secrets

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Authenticator abstracts the Bitwarden Identity authentication flow.
// Implementations must keep password and 2FA intermediate state in memory only.
type Authenticator interface {
	Unlock(ctx context.Context, email, password string) (session string, need2FA bool, err error)
	Verify2FA(ctx context.Context, code string, rememberDevice bool) (session string, err error)
}

// StubAuthenticator is used by unit tests and unconfigured development setups.
type StubAuthenticator struct {
	Password string
	TOTP     string
	Token    string

	mu    sync.Mutex
	state string
}

func NewStubAuthenticator(password, totp, token string) *StubAuthenticator {
	if token == "" {
		token = "bw-stub-session"
	}
	return &StubAuthenticator{Password: password, TOTP: totp, Token: token}
}

func (a *StubAuthenticator) Unlock(_ context.Context, email, password string) (string, bool, error) {
	if strings.TrimSpace(email) == "" {
		return "", false, fmt.Errorf("邮箱不能为空")
	}
	if password == "" {
		return "", false, fmt.Errorf("主密码不能为空")
	}
	if a.Password != "" && password != a.Password {
		return "", false, fmt.Errorf("主密码不正确")
	}
	if a.TOTP != "" {
		a.mu.Lock()
		a.state = "awaiting_2fa"
		a.mu.Unlock()
		return "", true, nil
	}
	return a.Token, false, nil
}

func (a *StubAuthenticator) Verify2FA(_ context.Context, code string, _ bool) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != "awaiting_2fa" {
		return "", fmt.Errorf("当前不需要二次验证")
	}
	if code == "" {
		return "", fmt.Errorf("验证码不能为空")
	}
	if a.TOTP != "" && code != a.TOTP {
		return "", fmt.Errorf("验证码不正确")
	}
	a.state = ""
	return a.Token, nil
}
