package localcert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DNSPod 是 DNSPod 解析的最小客户端，只实现签发与部署需要的三件事：
// 发布 TXT、删除记录、确保 A 记录指向指定 IP。
//
// 用 DNSPod 传统 API（dnsapi.cn）与 API Token（形如 "ID,Token"），
// 不需要实现腾讯云 API v3 的签名流程。凭据由云端从 Dec 密钥里读取，
// 不下发到任何设备（需求 4.4）。
type DNSPod struct {
	// Token 是 "ID,Token" 形式的 API Token。
	Token string
	// Zone 是托管在 DNSPod 上的主域名，例如 firoyang.com。
	Zone string
	// Endpoint 为空时使用 https://dnsapi.cn。
	Endpoint string
	HTTP     *http.Client
	// TTL 是新建记录的 TTL（秒），为 0 时取 600。
	TTL int
}

// ErrDNSPod 包装 DNSPod 返回的业务错误。
var ErrDNSPod = errors.New("dnspod")

type dnspodStatus struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type dnspodRecord struct {
	ID    json.Number `json:"id"`
	Name  string      `json:"name"`
	Type  string      `json:"type"`
	Value string      `json:"value"`
	Line  string      `json:"line"`
}

// subDomain 把 fqdn 转成相对 Zone 的主机记录。
func (d *DNSPod) subDomain(fqdn string) (string, error) {
	fqdn = strings.TrimSuffix(strings.ToLower(fqdn), ".")
	zone := strings.TrimSuffix(strings.ToLower(d.Zone), ".")
	if zone == "" {
		return "", errors.New("未配置 DNSPod 主域名")
	}
	if fqdn == zone {
		return "@", nil
	}
	if !strings.HasSuffix(fqdn, "."+zone) {
		return "", fmt.Errorf("%s 不属于主域名 %s", fqdn, zone)
	}
	return strings.TrimSuffix(fqdn, "."+zone), nil
}

func (d *DNSPod) call(ctx context.Context, action string, form url.Values, out any) error {
	if d.Token == "" {
		return ErrNotConfigured
	}
	endpoint := d.Endpoint
	if endpoint == "" {
		endpoint = "https://dnsapi.cn"
	}
	form.Set("login_token", d.Token)
	form.Set("format", "json")
	form.Set("lang", "cn")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(endpoint, "/")+"/"+action, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// DNSPod 要求带可识别的 User-Agent，否则可能被封禁。
	req.Header.Set("User-Agent", "dec-cloud/1.0 (dec.firoyang.com)")
	client := d.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("调用 DNSPod %s 失败: %w", action, err)
	}
	defer resp.Body.Close()
	var raw struct {
		Status dnspodStatus `json:"status"`
	}
	body := json.NewDecoder(resp.Body)
	var all json.RawMessage
	if err := body.Decode(&all); err != nil {
		return fmt.Errorf("解析 DNSPod %s 响应失败: %w", action, err)
	}
	if err := json.Unmarshal(all, &raw); err != nil {
		return err
	}
	if raw.Status.Code != "1" {
		return fmt.Errorf("%w: %s 返回 %s %s", ErrDNSPod, action, raw.Status.Code, raw.Status.Message)
	}
	if out != nil {
		return json.Unmarshal(all, out)
	}
	return nil
}

// Present 创建 TXT 记录，返回删除它的清理函数。
func (d *DNSPod) Present(ctx context.Context, fqdn, value string) (func(context.Context) error, error) {
	sub, err := d.subDomain(fqdn)
	if err != nil {
		return nil, err
	}
	ttl := d.TTL
	if ttl <= 0 {
		ttl = 600
	}
	var out struct {
		Record dnspodRecord `json:"record"`
	}
	form := url.Values{
		"domain":      {d.Zone},
		"sub_domain":  {sub},
		"record_type": {"TXT"},
		"record_line": {"默认"},
		"value":       {value},
		"ttl":         {fmt.Sprint(ttl)},
	}
	if err := d.call(ctx, "Record.Create", form, &out); err != nil {
		return nil, err
	}
	id := out.Record.ID.String()
	return func(ctx context.Context) error {
		return d.call(ctx, "Record.Remove", url.Values{"domain": {d.Zone}, "record_id": {id}}, nil)
	}, nil
}

// list 列出主机记录 sub 下指定类型的记录。不存在时返回空。
func (d *DNSPod) list(ctx context.Context, sub, typ string) ([]dnspodRecord, error) {
	var out struct {
		Records []dnspodRecord `json:"records"`
	}
	err := d.call(ctx, "Record.List", url.Values{
		"domain":      {d.Zone},
		"sub_domain":  {sub},
		"record_type": {typ},
	}, &out)
	if err != nil {
		// 10 表示「记录列表为空」，不是错误。
		if errors.Is(err, ErrDNSPod) && strings.Contains(err.Error(), "返回 10 ") {
			return nil, nil
		}
		return nil, err
	}
	return out.Records, nil
}

// EnsureA 确保 fqdn 的 A 记录只有一条且指向 ip，返回是否做了修改。
//
// 用于部署时保证 local.dec.firoyang.com 指向 127.0.0.1（需求 4.3），
// 幂等：已经正确时不做任何写操作。
func (d *DNSPod) EnsureA(ctx context.Context, fqdn, ip string) (bool, error) {
	sub, err := d.subDomain(fqdn)
	if err != nil {
		return false, err
	}
	recs, err := d.list(ctx, sub, "A")
	if err != nil {
		return false, err
	}
	if len(recs) == 1 && recs[0].Value == ip {
		return false, nil
	}
	if len(recs) == 0 {
		return true, d.call(ctx, "Record.Create", url.Values{
			"domain":      {d.Zone},
			"sub_domain":  {sub},
			"record_type": {"A"},
			"record_line": {"默认"},
			"value":       {ip},
			"ttl":         {"600"},
		}, nil)
	}
	// 保留第一条并改值，删掉多余的，避免解析结果随机落到别的地址。
	if err := d.call(ctx, "Record.Modify", url.Values{
		"domain":      {d.Zone},
		"record_id":   {recs[0].ID.String()},
		"sub_domain":  {sub},
		"record_type": {"A"},
		"record_line": {"默认"},
		"value":       {ip},
	}, nil); err != nil {
		return false, err
	}
	for _, r := range recs[1:] {
		if err := d.call(ctx, "Record.Remove", url.Values{"domain": {d.Zone}, "record_id": {r.ID.String()}}, nil); err != nil {
			return true, err
		}
	}
	return true, nil
}
