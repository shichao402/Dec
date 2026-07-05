package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	twoFactorProviderAuthenticator = "0"
)

type preloginResponse struct {
	Kdf             int    `json:"kdf"`
	KdfIterations   int    `json:"kdfIterations"`
	KdfMemory       *int   `json:"kdfMemory"`
	KdfParallelism  *int   `json:"kdfParallelism"`
	Salt            string `json:"salt"`
}

type tokenErrorResponse struct {
	Error            string   `json:"error"`
	ErrorDescription string   `json:"error_description"`
	TwoFactorToken   string   `json:"twoFactorToken"`
	TwoFactorProviders []string `json:"twoFactorProviders"`
}

type tokenSuccessResponse struct {
	AccessToken string `json:"access_token"`
}

// IdentityClient 对接 Bitwarden Identity（prelogin + token）。
type IdentityClient struct {
	IdentityURL string
	Email       string
	DeviceID    string
	HTTP        *http.Client
}

func (c *IdentityClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *IdentityClient) Prelogin(ctx context.Context) (*preloginResponse, error) {
	reqURL := c.IdentityURL + "/accounts/prelogin?email=" + url.QueryEscape(c.Email)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("Bitwarden prelogin 失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Bitwarden prelogin HTTP %d: %s", resp.StatusCode, trimBody(body))
	}
	var out preloginResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析 prelogin 响应失败: %w", err)
	}
	if out.KdfIterations <= 0 {
		out.KdfIterations = 600000
	}
	return &out, nil
}

func preloginSalt(pre *preloginResponse, email string) string {
	if pre != nil && strings.TrimSpace(pre.Salt) != "" {
		return pre.Salt
	}
	return email
}

type loginAttempt struct {
	accessToken      string
	need2FA          bool
	twoFactorSession string
}

func (c *IdentityClient) Login(ctx context.Context, password string, twoFactorCode, twoFactorSession string) (*loginAttempt, error) {
	pre, err := c.Prelogin(ctx)
	if err != nil {
		return nil, err
	}
	hash := masterPasswordHash(password, preloginSalt(pre, c.Email), pre.KdfIterations)
	form := c.baseTokenForm(hash)
	if twoFactorSession != "" {
		form.Set("token", twoFactorSession)
	}
	if twoFactorCode != "" {
		form.Set("twoFactorToken", twoFactorCode)
		form.Set("twoFactorProvider", twoFactorProviderAuthenticator)
		form.Set("twoFactorRemember", "0")
	}
	return c.postToken(ctx, form)
}

func (c *IdentityClient) baseTokenForm(passwordHash string) url.Values {
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("scope", "api offline_access")
	form.Set("client_id", "web")
	form.Set("username", c.Email)
	form.Set("password", passwordHash)
	form.Set("deviceType", "14")
	form.Set("deviceIdentifier", c.DeviceID)
	form.Set("deviceName", "Dec")
	return form
}

func (c *IdentityClient) postToken(ctx context.Context, form url.Values) (*loginAttempt, error) {
	reqURL := strings.TrimRight(c.IdentityURL, "/") + "/connect/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("Bitwarden 登录失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusOK {
		var ok tokenSuccessResponse
		if err := json.Unmarshal(body, &ok); err != nil {
			return nil, fmt.Errorf("解析 token 响应失败: %w", err)
		}
		if ok.AccessToken == "" {
			return nil, fmt.Errorf("Bitwarden 登录未返回 access_token")
		}
		return &loginAttempt{accessToken: ok.AccessToken}, nil
	}

	var errResp tokenErrorResponse
	_ = json.Unmarshal(body, &errResp)
	if errResp.TwoFactorToken != "" || len(errResp.TwoFactorProviders) > 0 {
		return &loginAttempt{
			need2FA:          true,
			twoFactorSession: errResp.TwoFactorToken,
		}, nil
	}
	msg := errResp.ErrorDescription
	if msg == "" {
		msg = errResp.Error
	}
	if msg == "" {
		msg = trimBody(body)
	}
	return nil, fmt.Errorf("Bitwarden 登录失败: %s", msg)
}

func trimBody(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
