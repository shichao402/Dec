package localcert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newFakeTencent 起一个假的腾讯云 v3 端点，记录收到的请求。
func newFakeTencent(t *testing.T) (*TencentDNSPod, *[]map[string]any, *[]http.Header) {
	t.Helper()
	var bodies []map[string]any
	var headers []http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		headers = append(headers, r.Header.Clone())

		action := r.Header.Get("X-TC-Action")
		w.Header().Set("Content-Type", "application/json")
		switch action {
		case "DescribeRecordList":
			json.NewEncoder(w).Encode(map[string]any{
				"Response": map[string]any{
					"RecordList": []map[string]any{
						{"RecordId": 101, "Name": "local", "Type": "A", "Value": "1.2.3.4"},
					},
				},
			})
		case "CreateRecord":
			json.NewEncoder(w).Encode(map[string]any{"Response": map[string]any{"RecordId": 202}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"Response": map[string]any{}})
		}
	}))
	t.Cleanup(srv.Close)

	d := &TencentDNSPod{
		SecretID:  "AKIDtest1234567890abcdef",
		SecretKey: "testsecretkey",
		Zone:      "example.com",
		Endpoint:  srv.URL,
	}
	return d, &bodies, &headers
}

// TestTencentDNSPodSignsRequest 确认请求带齐 v3 签名所需的头。
//
// 签名本身由腾讯云侧校验，这里守住的是「格式与字段齐全」：少了任何一个
// 头都会在真实调用时得到 AuthFailure，而那种错误很难从日志反推到缺什么。
func TestTencentDNSPodSignsRequest(t *testing.T) {
	d, bodies, headers := newFakeTencent(t)
	d.Now = func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }

	if _, err := d.EnsureA(context.Background(), "local.example.com", "127.0.0.1"); err != nil {
		t.Fatalf("EnsureA 失败: %v", err)
	}

	h := (*headers)[0]
	auth := h.Get("Authorization")
	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 ") {
		t.Errorf("Authorization 前缀应为 TC3-HMAC-SHA256，实际 %q", auth)
	}
	for _, want := range []string{
		"Credential=AKIDtest1234567890abcdef/2026-10-23/dnspod/tc3_request",
		"SignedHeaders=content-type;host",
		"Signature=",
	} {
		if !strings.Contains(auth, want) {
			t.Errorf("Authorization 缺少 %q，实际 %q", want, auth)
		}
	}
	if got := h.Get("X-TC-Action"); got != "DescribeRecordList" {
		t.Errorf("第一次调用应是 DescribeRecordList，实际 %q", got)
	}
	if got := h.Get("X-TC-Version"); got != "2021-03-23" {
		t.Errorf("版本应为 2021-03-23，实际 %q", got)
	}
	if got := h.Get("X-TC-Timestamp"); got != "1792756800" {
		t.Errorf("时间戳应取自注入的 Now（2026-10-23T12:00:00Z），实际 %q", got)
	}
	// 签名绝不能出现在请求体里，否则会进日志。
	for i, b := range *bodies {
		if raw, _ := json.Marshal(b); strings.Contains(string(raw), "testsecretkey") {
			t.Errorf("第 %d 个请求体里出现了 SecretKey", i)
		}
	}
}

// TestTencentDNSPodEnsureA 覆盖三种情形：新建、改值、已一致。
func TestTencentDNSPodEnsureA(t *testing.T) {
	t.Run("已存在不同值时改写", func(t *testing.T) {
		d, bodies, _ := newFakeTencent(t)
		changed, err := d.EnsureA(context.Background(), "local.example.com", "127.0.0.1")
		if err != nil {
			t.Fatalf("EnsureA 失败: %v", err)
		}
		// 假端点返回一条 1.2.3.4 的记录，所以必须改写。
		if !changed {
			t.Error("存在不同值的 A 记录时应报告已变更")
		}
		if len(*bodies) != 2 {
			t.Fatalf("应恰好两次调用（查 + 改），实际 %d 次", len(*bodies))
		}
		if got := (*bodies)[1]["Value"]; got != "127.0.0.1" {
			t.Errorf("改写后的值应为 127.0.0.1，实际 %v", got)
		}
	})

	t.Run("子域不在主域名下时报错", func(t *testing.T) {
		d, _, _ := newFakeTencent(t)
		if _, err := d.EnsureA(context.Background(), "local.other.com", "127.0.0.1"); err == nil {
			t.Error("跨主域名的子域应报错")
		}
	})

	t.Run("缺少凭据时报错", func(t *testing.T) {
		d := &TencentDNSPod{Zone: "example.com"}
		if _, err := d.Present(context.Background(), "local.example.com", "x"); err == nil {
			t.Error("缺少 SecretId/SecretKey 应报错")
		}
	})
}

// TestTencentDNSPodPresentCleanup 确认 TXT 发布后能按记录 ID 删除。
func TestTencentDNSPodPresentCleanup(t *testing.T) {
	d, bodies, headers := newFakeTencent(t)
	cleanup, err := d.Present(context.Background(), "_acme-challenge.example.com", "token")
	if err != nil {
		t.Fatalf("Present 失败: %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("应只调用一次 CreateRecord，实际 %d 次", len(*bodies))
	}
	created := (*bodies)[0]
	if created["RecordType"] != "TXT" || created["Value"] != "token" {
		t.Errorf("CreateRecord 参数不对: %v", created)
	}
	if created["SubDomain"] != "_acme-challenge" {
		t.Errorf("SubDomain 应去掉主域名，实际 %v", created["SubDomain"])
	}

	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup 失败: %v", err)
	}
	if got := (*headers)[len(*headers)-1].Get("X-TC-Action"); got != "DeleteRecord" {
		t.Errorf("清理应调用 DeleteRecord，实际 %q", got)
	}
	if got := (*bodies)[len(*bodies)-1]["RecordId"]; got != float64(202) {
		t.Errorf("删除的记录 ID 应来自 CreateRecord 返回值，实际 %v", got)
	}
}

// TestTencentDNSPodPropagatesAPIError 确认业务错误不会被当成成功。
func TestTencentDNSPodPropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{
				"Error": map[string]any{
					"Code":    "AuthFailure.SignatureFailure",
					"Message": "签名错误",
				},
				"RequestId": "req-1",
			},
		})
	}))
	defer srv.Close()

	d := &TencentDNSPod{
		SecretID: "AKIDx", SecretKey: "k", Zone: "example.com", Endpoint: srv.URL,
	}
	_, err := d.EnsureA(context.Background(), "local.example.com", "127.0.0.1")
	if err == nil {
		t.Fatal("API 返回 Error 时必须报错")
	}
	if !strings.Contains(err.Error(), "AuthFailure.SignatureFailure") {
		t.Errorf("错误里应带上 API 的错误码，实际 %v", err)
	}
}

// TestDetectCredential 守住两种凭据格式的识别边界。
//
// 识别错了的后果是签发失败且原因难查，所以每种形态都要有正向与反向用例。
func TestDetectCredential(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantKind string
		wantID   string
		wantKey  string
	}{
		{
			name:     "传统 Token",
			raw:      "123456,abcdef0123456789",
			wantKind: CredLegacy,
			wantID:   "123456",
			wantKey:  "abcdef0123456789",
		},
		{
			name:     "传统 Token 带空格",
			raw:      "  123456,abcdef  \n",
			wantKind: CredLegacy,
			wantID:   "123456",
			wantKey:  "abcdef",
		},
		{
			name:     "腾讯云 JSON",
			raw:      `{"secret_id":"AKIDabcdefghij1234567890","secret_key":"mykey"}`,
			wantKind: CredTencent,
			wantID:   "AKIDabcdefghij1234567890",
			wantKey:  "mykey",
		},
		{
			name:     "腾讯云两行",
			raw:      "AKIDabcdefghij1234567890\nmykey",
			wantKind: CredTencentEnv,
			wantID:   "AKIDabcdefghij1234567890",
			wantKey:  "mykey",
		},
		{
			name:     "空内容",
			raw:      "   ",
			wantKind: "",
		},
		{
			name:     "认不出来",
			raw:      "随便一段文字",
			wantKind: "",
		},
		{
			name:     "JSON 缺字段",
			raw:      `{"secret_id":"AKIDabcdefghij1234567890"}`,
			wantKind: "",
		},
		{
			name:     "逗号前不是数字",
			raw:      "abc,def",
			wantKind: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, id, key := DetectCredential(c.raw)
			if kind != c.wantKind {
				t.Fatalf("形态应为 %q，实际 %q", c.wantKind, kind)
			}
			if id != c.wantID {
				t.Errorf("ID 应为 %q，实际 %q", c.wantID, id)
			}
			if key != c.wantKey {
				t.Errorf("密钥应为 %q，实际 %q", c.wantKey, key)
			}
		})
	}
}

// TestDNSPodFor 确认工厂按形态给出对应实现。
func TestDNSPodFor(t *testing.T) {
	if p, kind, err := DNSPodFor("123456,abcdef", "example.com"); err != nil {
		t.Errorf("传统 Token 不应报错: %v", err)
	} else {
		if _, ok := p.(*DNSPod); !ok {
			t.Errorf("传统 Token 应得到 *DNSPod，实际 %T", p)
		}
		if kind != CredLegacy {
			t.Errorf("形态应为 %q，实际 %q", CredLegacy, kind)
		}
	}

	if p, kind, err := DNSPodFor(`{"secret_id":"AKIDabcdefghij1234567890","secret_key":"k"}`, "example.com"); err != nil {
		t.Errorf("腾讯云密钥不应报错: %v", err)
	} else {
		if _, ok := p.(*TencentDNSPod); !ok {
			t.Errorf("腾讯云密钥应得到 *TencentDNSPod，实际 %T", p)
		}
		if kind != CredTencent {
			t.Errorf("形态应为 %q，实际 %q", CredTencent, kind)
		}
	}

	if _, _, err := DNSPodFor("认不出来", "example.com"); err == nil {
		t.Error("无法识别时应报错")
	}
}

// TestMaskSecret 确认日志里不会漏出完整密钥。
func TestMaskSecret(t *testing.T) {
	got := MaskSecret("AKIDabcdefghij1234567890", "supersecretkey")
	if strings.Contains(got, "supersecretkey") {
		t.Errorf("掩码后不该包含密钥内容: %q", got)
	}
	if strings.Contains(got, "AKIDabcdefghij1234567890") {
		t.Errorf("掩码后不该包含完整 SecretId: %q", got)
	}
	if !strings.Contains(got, "AKIDabcd") {
		t.Errorf("应保留可辨识的前缀: %q", got)
	}
}
