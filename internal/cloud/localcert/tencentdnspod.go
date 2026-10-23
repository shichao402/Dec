package localcert

// 腾讯云 API v3 的 DNSPod 客户端。
//
// 已有的 DNSPod 类型走 dnsapi.cn 的传统 API（凭据形如 "ID,Token"），
// 那是 DNSPod 被腾讯云收购前的独立体系。本机部署里能拿到的凭据是
// 腾讯云 API 密钥（SecretId/SecretKey），两者不能互换：
//
//   - 传统 API Token：DNSPod 控制台用户中心 → 密钥管理生成，认证方式是
//     请求体里带 login_token。
//   - 腾讯云 API v3：CAM 密钥，认证方式是 TC3-HMAC-SHA256 签名，
//     同一个密钥还能管 CVM、COS 等，权限由 CAM 策略控制。
//
// 用户手上只有后者，且后者更规范（可审计、可细粒度授权、可轮换），
// 所以这里补一个 v3 客户端，保持 DNSProvider 接口不变。

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TencentDNSPod 是腾讯云 API v3 的 DNSPod 客户端。
//
// 实现 DNSProvider（Present）以及部署需要的 EnsureA，与 DNSPod 等价。
type TencentDNSPod struct {
	SecretID  string
	SecretKey string
	// Zone 是托管在 DNSPod 上的主域名，例如 firoyang.com。
	Zone string
	// Region 通常为空：DNSPod 是全局服务。
	Region string
	// Endpoint 为空时使用 dnspod.tencentcloudapi.com。
	Endpoint string
	HTTP     *http.Client
	// TTL 是新建记录的 TTL（秒），为 0 时取 600。
	TTL int
	// Now 便于测试注入时间。
	Now func() time.Time
}

const (
	tcService = "dnspod"
	tcVersion = "2021-03-23"
	tcHost    = "dnspod.tencentcloudapi.com"
)

// tcResponse 是所有 v3 响应的外层。
type tcResponse struct {
	Response struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
		RequestID string `json:"RequestId"`
	} `json:"Response"`
}

func (d *TencentDNSPod) client() *http.Client {
	if d.HTTP != nil {
		return d.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (d *TencentDNSPod) endpoint() string {
	if d.Endpoint != "" {
		return d.Endpoint
	}
	return "https://" + tcHost
}

func (d *TencentDNSPod) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

func (d *TencentDNSPod) ttl() int {
	if d.TTL > 0 {
		return d.TTL
	}
	return 600
}

// subDomain 把 fqdn 转成相对 Zone 的主机记录。
func (d *TencentDNSPod) subDomain(fqdn string) (string, error) {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	zone := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d.Zone)), ".")
	if zone == "" {
		return "", fmt.Errorf("%w：未指定主域名", ErrDNSPod)
	}
	if name == zone {
		return "@", nil
	}
	suffix := "." + zone
	if !strings.HasSuffix(name, suffix) {
		return "", fmt.Errorf("%w：%s 不属于主域名 %s", ErrDNSPod, fqdn, d.Zone)
	}
	sub := strings.TrimSuffix(name, suffix)
	if sub == "" {
		return "@", nil
	}
	return sub, nil
}

// call 用 TC3-HMAC-SHA256 签名调用一个 v3 接口。
//
// 签名流程见腾讯云 API v3 文档；这里只实现本文件用到的部分：
// POST + application/json，签 content-type 与 host 两个头。
func (d *TencentDNSPod) call(ctx context.Context, action string, payload any, out any) error {
	if d.SecretID == "" || d.SecretKey == "" {
		return fmt.Errorf("%w：缺少 SecretId 或 SecretKey", ErrDNSPod)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := d.now()
	date := now.Format("2006-01-02")
	timestamp := now.Unix()

	const contentType = "application/json; charset=utf-8"
	hashedPayload := sha256HexBytes(body)
	canonicalHeaders := "content-type:" + contentType + "\nhost:" + tcHost + "\n"
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{
		http.MethodPost, "/", "",
		canonicalHeaders, signedHeaders, hashedPayload,
	}, "\n")

	credentialScope := date + "/" + tcService + "/tc3_request"
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256", fmt.Sprint(timestamp), credentialScope,
		sha256HexString(canonicalRequest),
	}, "\n")

	secretDate := hmacSHA256([]byte("TC3"+d.SecretKey), date)
	secretService := hmacSHA256(secretDate, tcService)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	authorization := fmt.Sprintf(
		"TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		d.SecretID, credentialScope, signedHeaders, signature)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", tcVersion)
	req.Header.Set("X-TC-Timestamp", fmt.Sprint(timestamp))
	if d.Region != "" {
		req.Header.Set("X-TC-Region", d.Region)
	}

	resp, err := d.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w：%s 返回 HTTP %d: %s", ErrDNSPod, action, resp.StatusCode, truncate(string(raw), 300))
	}

	var envelope tcResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("%w：解析 %s 响应失败: %w", ErrDNSPod, action, err)
	}
	if e := envelope.Response.Error; e != nil {
		return fmt.Errorf("%w：%s 失败 %s: %s", ErrDNSPod, action, e.Code, e.Message)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%w：解析 %s 结果失败: %w", ErrDNSPod, action, err)
		}
	}
	return nil
}

type tcRecord struct {
	RecordID json.Number `json:"RecordId"`
	Name     string      `json:"Name"`
	Type     string      `json:"Type"`
	Value    string      `json:"Value"`
	Line     string      `json:"Line"`
	TTL      int         `json:"TTL"`
}

// list 返回 sub 下指定类型的记录。
func (d *TencentDNSPod) list(ctx context.Context, sub, typ string) ([]tcRecord, error) {
	var out struct {
		Response struct {
			RecordList []tcRecord `json:"RecordList"`
		} `json:"Response"`
	}
	payload := map[string]any{
		"Domain":     strings.TrimSuffix(strings.ToLower(d.Zone), "."),
		"Subdomain":  sub,
		"RecordType": typ,
	}
	if sub == "@" {
		payload["Subdomain"] = "@"
	}
	if err := d.call(ctx, "DescribeRecordList", payload, &out); err != nil {
		return nil, err
	}
	return out.Response.RecordList, nil
}

func (d *TencentDNSPod) create(ctx context.Context, sub, typ, value string) (string, error) {
	var out struct {
		Response struct {
			RecordID json.Number `json:"RecordId"`
		} `json:"Response"`
	}
	payload := map[string]any{
		"Domain":     strings.TrimSuffix(strings.ToLower(d.Zone), "."),
		"SubDomain":  sub,
		"RecordType": typ,
		"RecordLine": "默认",
		"Value":      value,
		"TTL":        d.ttl(),
	}
	if err := d.call(ctx, "CreateRecord", payload, &out); err != nil {
		return "", err
	}
	return out.Response.RecordID.String(), nil
}

func (d *TencentDNSPod) remove(ctx context.Context, recordID string) error {
	payload := map[string]any{
		"Domain":   strings.TrimSuffix(strings.ToLower(d.Zone), "."),
		"RecordId": json.Number(recordID),
	}
	return d.call(ctx, "DeleteRecord", payload, nil)
}

func (d *TencentDNSPod) modify(ctx context.Context, recordID, sub, typ, value string) error {
	payload := map[string]any{
		"Domain":     strings.TrimSuffix(strings.ToLower(d.Zone), "."),
		"SubDomain":  sub,
		"RecordId":   json.Number(recordID),
		"RecordType": typ,
		"RecordLine": "默认",
		"Value":      value,
		"TTL":        d.ttl(),
	}
	return d.call(ctx, "ModifyRecord", payload, nil)
}

// Present 发布 fqdn 的 TXT 记录，返回清理函数（实现 DNSProvider）。
func (d *TencentDNSPod) Present(ctx context.Context, fqdn, value string) (func(context.Context) error, error) {
	sub, err := d.subDomain(fqdn)
	if err != nil {
		return nil, err
	}
	id, err := d.create(ctx, sub, "TXT", value)
	if err != nil {
		return nil, err
	}
	return func(cleanupCtx context.Context) error {
		return d.remove(cleanupCtx, id)
	}, nil
}

// EnsureA 确保 fqdn 有一条指向 ip 的 A 记录，返回是否发生了变更。
//
// 与 DNSPod.EnsureA 语义一致：已存在同值记录时不动；存在不同值的记录时
// 改第一条、删多余的；完全没有时新建。
func (d *TencentDNSPod) EnsureA(ctx context.Context, fqdn, ip string) (bool, error) {
	sub, err := d.subDomain(fqdn)
	if err != nil {
		return false, err
	}
	recs, err := d.list(ctx, sub, "A")
	if err != nil {
		return false, err
	}
	if len(recs) == 0 {
		if _, err := d.create(ctx, sub, "A", ip); err != nil {
			return false, err
		}
		return true, nil
	}
	changed := false
	for i, r := range recs {
		if i == 0 {
			if r.Value == ip {
				continue
			}
			if err := d.modify(ctx, r.RecordID.String(), sub, "A", ip); err != nil {
				return changed, err
			}
			changed = true
			continue
		}
		// 多余的同名 A 记录会让解析结果不确定，删掉。
		if err := d.remove(ctx, r.RecordID.String()); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

func hmacSHA256(key []byte, msg string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return h.Sum(nil)
}

func sha256HexBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sha256HexString(s string) string {
	return sha256HexBytes([]byte(s))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// 保证接口实现，编译期检查。
var (
	_ DNSProvider = (*TencentDNSPod)(nil)
	_ DNSProvider = (*DNSPod)(nil)
)
