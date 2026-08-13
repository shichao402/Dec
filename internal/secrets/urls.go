package secrets

import (
	"fmt"
	"net/url"
	"strings"
)

// Endpoints 从 server_url 解析 Identity 与 Vault API 根地址。
func (c *Config) Endpoints() (identityURL, apiURL string, err error) {
	if c == nil || strings.TrimSpace(c.ServerURL) == "" {
		return "", "", fmt.Errorf("server_url 未配置")
	}
	base := strings.TrimRight(strings.TrimSpace(c.ServerURL), "/")
	u, err := url.Parse(base)
	if err != nil {
		return "", "", fmt.Errorf("解析 server_url 失败: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "vault.bitwarden.com":
		return "https://identity.bitwarden.com", "https://api.bitwarden.com", nil
	case "vault.bitwarden.eu":
		return "https://identity.bitwarden.eu", "https://api.bitwarden.eu", nil
	}
	if strings.HasSuffix(host, ".bitwarden.com") {
		return "https://identity.bitwarden.com", "https://api.bitwarden.com", nil
	}
	if strings.HasSuffix(host, ".bitwarden.eu") {
		return "https://identity.bitwarden.eu", "https://api.bitwarden.eu", nil
	}
	return base + "/identity", base + "/api", nil
}
