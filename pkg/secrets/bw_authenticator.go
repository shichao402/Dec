package secrets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"

	"github.com/shichao402/Dec/pkg/secrets/unlock"
)

// BWAuthenticator 通过 Bitwarden Identity API 完成 unlock / 2FA。
type BWAuthenticator struct {
	client *IdentityClient

	mu               sync.Mutex
	password         string
	twoFactorSession string
}

// NewBWAuthenticator 创建真实 Bitwarden Authenticator。
func NewBWAuthenticator(cfg *Config, deviceID string, httpClient *http.Client) (*BWAuthenticator, error) {
	if cfg == nil || !cfg.CanAuthenticate() {
		return nil, fmt.Errorf("Bitwarden 认证配置不完整")
	}
	identityURL, _, err := cfg.Endpoints()
	if err != nil {
		return nil, err
	}
	if deviceID == "" {
		deviceID = newDeviceID()
	}
	client := &IdentityClient{
		IdentityURL: identityURL,
		Email:       cfg.Email,
		DeviceID:    deviceID,
		HTTP:        httpClient,
	}
	return &BWAuthenticator{client: client}, nil
}

func (a *BWAuthenticator) Unlock(ctx context.Context, password string) (string, bool, error) {
	if password == "" {
		return "", false, fmt.Errorf("主密码不能为空")
	}
	a.mu.Lock()
	a.password = password
	a.twoFactorSession = ""
	a.mu.Unlock()

	attempt, err := a.client.Login(ctx, password, "", "")
	if err != nil {
		return "", false, err
	}
	if attempt.need2FA {
		a.mu.Lock()
		a.twoFactorSession = attempt.twoFactorSession
		a.mu.Unlock()
		return "", true, nil
	}
	a.mu.Lock()
	a.password = ""
	a.mu.Unlock()
	return attempt.accessToken, false, nil
}

func (a *BWAuthenticator) Verify2FA(ctx context.Context, code string) (string, error) {
	if code == "" {
		return "", fmt.Errorf("验证码不能为空")
	}
	a.mu.Lock()
	session := a.twoFactorSession
	password := a.password
	a.mu.Unlock()
	if session == "" {
		return "", fmt.Errorf("当前不需要二次验证")
	}
	if password == "" {
		return "", fmt.Errorf("二次验证会话已失效，请重新输入主密码")
	}
	attempt, err := a.client.Login(ctx, password, code, session)
	if err != nil {
		return "", err
	}
	if attempt.need2FA {
		return "", fmt.Errorf("验证码不正确")
	}
	a.mu.Lock()
	a.twoFactorSession = ""
	a.password = ""
	a.mu.Unlock()
	return attempt.accessToken, nil
}

var _ unlock.Authenticator = (*BWAuthenticator)(nil)

func newDeviceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
