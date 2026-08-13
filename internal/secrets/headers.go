package secrets

import (
	"net/http"
)

// Bitwarden API 要求第三方客户端携带 Bitwarden-Client-Version（yyyy.mm.r 日历版本），
// 否则 Identity / Vault 请求会被拒绝。格式与官方 CLI/SDK 一致，见：
// https://bitwarden.com/help/versioning/
const (
	bitwardenClientName    = "cli"
	bitwardenClientVersion = "2026.7.0"
)

// bitwardenDeviceType 与 Identity token 表单中的 deviceType 保持一致。
const bitwardenDeviceType = "14"

func applyBitwardenHeaders(req *http.Request) {
	req.Header.Set("Bitwarden-Client-Version", bitwardenClientVersion)
	req.Header.Set("Bitwarden-Client-Name", bitwardenClientName)
	req.Header.Set("Device-Type", bitwardenDeviceType)
	req.Header.Set("User-Agent", "Dec/"+bitwardenClientVersion)
	if req.Method == http.MethodGet {
		req.Header.Set("Cache-Control", "no-store")
		req.Header.Set("Pragma", "no-cache")
	}
}
