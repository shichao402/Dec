package secrets

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/shichao402/Dec/pkg/secrets/unlock"
)

// BWAuthenticator 通过 Bitwarden Identity API 完成 unlock / 2FA。
type BWAuthenticator struct {
	cfg    *Config
	client *IdentityClient
	email  string

	mu                sync.Mutex
	password          string
	twoFactorSession  string
	awaiting2FA       bool
	twoFactorProvider string
}

// NewBWAuthenticator 创建真实 Bitwarden Authenticator。
// deviceID 为空时从 ~/.dec/secrets/device.json 加载或生成持久化 identifier。
func NewBWAuthenticator(cfg *Config, deviceID string, httpClient *http.Client) (*BWAuthenticator, error) {
	if cfg == nil || strings.TrimSpace(cfg.ServerURL) == "" {
		return nil, fmt.Errorf("Bitwarden 认证配置不完整")
	}
	identityURL, _, err := cfg.Endpoints()
	if err != nil {
		return nil, err
	}
	if deviceID == "" {
		deviceID, err = EnsureDeviceIdentifier()
		if err != nil {
			return nil, err
		}
	}
	client := &IdentityClient{
		IdentityURL: identityURL,
		Email:       cfg.Email,
		DeviceID:    deviceID,
		HTTP:        httpClient,
	}
	return &BWAuthenticator{cfg: cfg, client: client, email: cfg.Email}, nil
}

func (a *BWAuthenticator) Unlock(ctx context.Context, email, password string) (string, bool, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", false, fmt.Errorf("邮箱不能为空")
	}
	if password == "" {
		return "", false, fmt.Errorf("主密码不能为空")
	}
	a.mu.Lock()
	a.password = password
	a.email = email
	a.client.Email = email
	a.twoFactorSession = ""
	a.awaiting2FA = false
	a.twoFactorProvider = ""
	a.mu.Unlock()

	rememberToken, err := RememberToken(a.email)
	if err != nil {
		return "", false, err
	}

	opts := LoginOptions{}
	if rememberToken != "" {
		opts.RememberToken = rememberToken
	}
	attempt, err := a.client.Login(ctx, password, "", "", "", opts)
	if err != nil {
		return "", false, err
	}
	if attempt.need2FA {
		if rememberToken != "" {
			_ = ClearRememberToken(a.email)
		}
		a.mu.Lock()
		a.twoFactorSession = attempt.twoFactorSession
		a.awaiting2FA = true
		a.twoFactorProvider = attempt.twoFactorProvider
		if a.twoFactorProvider == "" {
			a.twoFactorProvider = twoFactorProviderAuthenticator
		}
		a.mu.Unlock()
		return "", true, nil
	}
	token, err := a.completeLogin(ctx, password, attempt.accessToken, attempt.twoFactorRemember)
	if err != nil {
		return "", false, err
	}
	return token, false, nil
}

func (a *BWAuthenticator) Verify2FA(ctx context.Context, code string, rememberDevice bool) (string, error) {
	if code == "" {
		return "", fmt.Errorf("验证码不能为空")
	}
	a.mu.Lock()
	session := a.twoFactorSession
	password := a.password
	provider := a.twoFactorProvider
	awaiting := a.awaiting2FA
	a.mu.Unlock()
	if !awaiting {
		return "", fmt.Errorf("当前不需要二次验证")
	}
	if password == "" {
		return "", fmt.Errorf("二次验证会话已失效，请重新输入主密码")
	}
	attempt, err := a.client.Login(ctx, password, code, session, provider, LoginOptions{RememberDevice: rememberDevice})
	if err != nil {
		return "", err
	}
	if attempt.need2FA {
		return "", fmt.Errorf("验证码不正确")
	}
	token, err := a.completeLogin(ctx, password, attempt.accessToken, attempt.twoFactorRemember)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (a *BWAuthenticator) completeLogin(ctx context.Context, password, accessToken, rememberToken string) (string, error) {
	if err := unlockVaultKey(ctx, a.cfg, a.client, password, accessToken); err != nil {
		return "", err
	}
	if err := SaveEmail(a.email); err != nil {
		return "", err
	}
	if err := a.persistRememberToken(rememberToken); err != nil {
		return "", err
	}
	a.mu.Lock()
	a.password = ""
	a.twoFactorSession = ""
	a.awaiting2FA = false
	a.twoFactorProvider = ""
	a.mu.Unlock()
	return accessToken, nil
}

func (a *BWAuthenticator) persistRememberToken(token string) error {
	if token == "" {
		return nil
	}
	return SetRememberToken(a.email, token)
}

var _ unlock.Authenticator = (*BWAuthenticator)(nil)
