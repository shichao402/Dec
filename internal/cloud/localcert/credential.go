package localcert

// 凭据格式识别：同一条云端密钥要能承载两种 DNSPod 凭据。
//
// DNSPod 有两套并存的 API，凭据形态完全不同：
//
//	传统 API（dnsapi.cn）  "ID,Token"            → DNSPod
//	腾讯云 API v3（TC3）    SecretId + SecretKey   → TencentDNSPod
//
// 用户手上可能只有其中一种。把识别放在这里而不是让部署时手选，是因为
// 格式本身足够有区分度：传统 Token 是「纯数字,字母数字」；API v3 的
// SecretId 固定以 AKID 开头，且长度明显更长。误判代价只是签发失败一次，
// 而让用户在部署时选错类型、再回来改配置的成本更高。

import (
	"context"
	"encoding/json"
	"strings"
)

// ZoneDNS 是本机证书签发用到的 DNS 操作全集。
//
// 比 ACME 需要的 DNSProvider 多一个 EnsureA：local 子域必须固定指向
// 127.0.0.1，这件事顺手在同一次签发里校正。
type ZoneDNS interface {
	DNSProvider
	EnsureA(ctx context.Context, fqdn, ip string) (bool, error)
}

// 编译期确认两个实现都满足 ZoneDNS。
var (
	_ ZoneDNS = (*DNSPod)(nil)
	_ ZoneDNS = (*TencentDNSPod)(nil)
)

// 凭据形态。
const (
	// CredLegacy 是 DNSPod 传统 API Token，形如 "123456,abcdef..."。
	CredLegacy = "dnspod-legacy"
	// CredTencent 是腾讯云 API v3 密钥对，JSON：{"secret_id":"AKID...","secret_key":"..."}。
	CredTencent = "tencent-api-v3"
	// CredTencentEnv 是腾讯云密钥对，用换行分隔两段（AKID...\n密钥）。
	CredTencentEnv = "tencent-api-v3-lines"
)

// DetectCredential 判断凭据形态，返回形态与解析出的字段。
//
// 认不出来时返回空形态：调用方据此给出「格式不认识」而非「权限不足」，
// 避免把配置问题误导成权限问题。
func DetectCredential(raw string) (kind, id, secret string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", ""
	}

	// JSON：{"secret_id": "...", "secret_key": "..."}
	if strings.HasPrefix(s, "{") {
		var obj struct {
			SecretID  string `json:"secret_id"`
			SecretKey string `json:"secret_key"`
		}
		if err := json.Unmarshal([]byte(s), &obj); err == nil {
			id := strings.TrimSpace(obj.SecretID)
			key := strings.TrimSpace(obj.SecretKey)
			if id != "" && key != "" {
				return CredTencent, id, key
			}
		}
	}

	// 换行或空格分隔两段，且第一段以 AKID 开头。
	if fields := strings.Fields(s); len(fields) == 2 && isTencentID(fields[0]) {
		return CredTencentEnv, fields[0], fields[1]
	}

	// 传统 Token："ID,Token"，ID 是纯数字。
	if i := strings.Index(s, ","); i > 0 && i < len(s)-1 {
		head := s[:i]
		if isDigits(head) {
			return CredLegacy, head, strings.TrimSpace(s[i+1:])
		}
	}

	return "", "", ""
}

// DNSPodFor 按凭据内容构造对应的 DNS 客户端。
//
// 返回的 err 是「格式不认识」这类配置错误，不是网络错误。
func DNSPodFor(raw, zone string) (ZoneDNS, string, error) {
	kind, id, secret := DetectCredential(raw)
	switch kind {
	case CredLegacy:
		return &DNSPod{Token: strings.TrimSpace(raw), Zone: zone}, kind, nil
	case CredTencent, CredTencentEnv:
		return &TencentDNSPod{SecretID: id, SecretKey: secret, Zone: zone}, kind, nil
	default:
		return nil, "", ErrUnrecognizedCredential
	}
}

// ErrUnrecognizedCredential 表示密钥内容不像任何一种已知的 DNSPod 凭据。
var ErrUnrecognizedCredential = errUnrecognizedCredential{}

type errUnrecognizedCredential struct{}

func (errUnrecognizedCredential) Error() string {
	return "DNSPod 凭据格式无法识别：需要 \"ID,Token\"（传统 API Token）" +
		"，或腾讯云 API 密钥对（{\"secret_id\":\"AKID...\",\"secret_key\":\"...\"}）"
}

// MaskSecret 把凭据里不该进日志的部分隐去，只留可辨识的前缀。
func MaskSecret(id, secret string) string {
	if id == "" {
		return "(空)"
	}
	shown := id
	if len(shown) > 8 {
		shown = shown[:8] + "..."
	}
	return shown + " (密钥长度 " + itoaLen(secret) + ")"
}

func isTencentID(s string) bool {
	return strings.HasPrefix(s, "AKID") && len(s) >= 20
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func itoaLen(s string) string {
	n := len(s)
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
