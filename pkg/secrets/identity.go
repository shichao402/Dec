package secrets

import (
	"bytes"
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
	twoFactorProviderRemember      = "5"
)

type preloginResponse struct {
	Kdf             int    `json:"kdf"`
	KdfIterations   int    `json:"kdfIterations"`
	KdfMemory       *int   `json:"kdfMemory"`
	KdfParallelism  *int   `json:"kdfParallelism"`
	Salt            string `json:"salt"`
}

type tokenErrorResponse struct {
	Error               string                     `json:"error"`
	ErrorDescription    string                     `json:"error_description"`
	TwoFactorToken      string                     `json:"twoFactorToken"`
	TwoFactorProviders  []string                   `json:"twoFactorProviders"`
	TwoFactorProviders2 map[string]json.RawMessage `json:"TwoFactorProviders2"`
}

func (r tokenErrorResponse) requires2FA() bool {
	if r.TwoFactorToken != "" {
		return true
	}
	if len(r.TwoFactorProviders) > 0 {
		return true
	}
	if len(r.TwoFactorProviders2) > 0 {
		return true
	}
	desc := strings.ToLower(r.ErrorDescription + " " + r.Error)
	return strings.Contains(desc, "two factor") ||
		strings.Contains(desc, "two-factor") ||
		strings.Contains(desc, "2fa")
}

func (r tokenErrorResponse) preferredProvider() string {
	for _, p := range r.TwoFactorProviders {
		if p == twoFactorProviderAuthenticator {
			return p
		}
	}
	if _, ok := r.TwoFactorProviders2[twoFactorProviderAuthenticator]; ok {
		return twoFactorProviderAuthenticator
	}
	if len(r.TwoFactorProviders) > 0 {
		return r.TwoFactorProviders[0]
	}
	for k := range r.TwoFactorProviders2 {
		return k
	}
	return twoFactorProviderAuthenticator
}

type tokenSuccessResponse struct {
	AccessToken   string `json:"access_token"`
	TwoFactorToken string `json:"TwoFactorToken"`
}

// LoginOptions 控制 token 请求中的 2FA / 设备信任行为。
type LoginOptions struct {
	// RememberDevice 在提交 TOTP 时设置 twoFactorRemember=1。
	RememberDevice bool
	// RememberToken 使用 Remember provider 跳过 2FA（与 bw 一致）。
	RememberToken string
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
	payload, err := json.Marshal(map[string]string{"email": c.Email})
	if err != nil {
		return nil, err
	}
	reqURL := strings.TrimRight(c.IdentityURL, "/") + "/accounts/prelogin"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	applyBitwardenHeaders(req)
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
	accessToken        string
	need2FA            bool
	twoFactorSession   string
	twoFactorProvider  string
	twoFactorRemember  string
}

func (c *IdentityClient) Login(ctx context.Context, password string, twoFactorCode, twoFactorSession, twoFactorProvider string, opts LoginOptions) (*loginAttempt, error) {
	pre, err := c.Prelogin(ctx)
	if err != nil {
		return nil, err
	}
	hash := masterPasswordHash(password, preloginSalt(pre, c.Email), pre.KdfIterations)
	form := c.baseTokenForm(hash)
	if twoFactorSession != "" {
		form.Set("token", twoFactorSession)
	}
	if opts.RememberToken != "" {
		form.Set("twoFactorToken", opts.RememberToken)
		form.Set("twoFactorProvider", twoFactorProviderRemember)
	} else if twoFactorCode != "" {
		provider := twoFactorProvider
		if provider == "" {
			provider = twoFactorProviderAuthenticator
		}
		form.Set("twoFactorToken", twoFactorCode)
		form.Set("twoFactorProvider", provider)
		if opts.RememberDevice {
			form.Set("twoFactorRemember", "1")
		} else {
			form.Set("twoFactorRemember", "0")
		}
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
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	applyBitwardenHeaders(req)

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
		return &loginAttempt{
			accessToken:       ok.AccessToken,
			twoFactorRemember: ok.TwoFactorToken,
		}, nil
	}

	var errResp tokenErrorResponse
	_ = json.Unmarshal(body, &errResp)
	if errResp.requires2FA() {
		return &loginAttempt{
			need2FA:           true,
			twoFactorSession:  errResp.TwoFactorToken,
			twoFactorProvider: errResp.preferredProvider(),
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
