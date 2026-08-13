package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type bwProfile struct {
	Key string `json:"key"`
}

// unlockVaultKey 在登录成功后拉取 profile.key 并解密 vault symmetric key 到进程内存。
func unlockVaultKey(ctx context.Context, cfg *Config, identity *IdentityClient, password, accessToken string) error {
	if cfg == nil || identity == nil {
		return fmt.Errorf("Bitwarden 配置不完整")
	}
	pre, err := identity.Prelogin(ctx)
	if err != nil {
		return err
	}
	encryptedKey, err := fetchProfileKey(ctx, cfg, accessToken, identity.httpClient())
	if err != nil {
		return err
	}
	key, err := decryptUserKey(encryptedKey, password, preloginSalt(pre, identity.Email), pre.KdfIterations)
	if err != nil {
		return err
	}
	SetUserKey(key)
	return nil
}

func fetchProfileKey(ctx context.Context, cfg *Config, token string, httpClient *http.Client) (string, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	_, apiURL, err := cfg.Endpoints()
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/accounts/profile", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	applyBitwardenHeaders(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("读取 Bitwarden profile 失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("读取 Bitwarden profile HTTP %d: %s", resp.StatusCode, trimBody(body))
	}
	var profile bwProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return "", fmt.Errorf("解析 Bitwarden profile 失败: %w", err)
	}
	if profile.Key == "" {
		return "", fmt.Errorf("Bitwarden profile 未返回 user key")
	}
	return profile.Key, nil
}
