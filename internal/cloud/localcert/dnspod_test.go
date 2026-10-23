package localcert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeDNSPod 模拟 dnsapi.cn 的 Record.* 接口。
type fakeDNSPod struct {
	mu      sync.Mutex
	nextID  int
	records map[string]dnspodRecord // id -> record
	calls   []string
	token   string
}

func newFakeDNSPod() *fakeDNSPod {
	return &fakeDNSPod{records: map[string]dnspodRecord{}, nextID: 100, token: "1,abc"}
}

func (f *fakeDNSPod) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	action := strings.TrimPrefix(r.URL.Path, "/")
	f.calls = append(f.calls, action)
	reply := func(code, msg string, extra map[string]any) {
		out := map[string]any{"status": map[string]string{"code": code, "message": msg}}
		for k, v := range extra {
			out[k] = v
		}
		_ = json.NewEncoder(w).Encode(out)
	}
	if r.Form.Get("login_token") != f.token || r.Form.Get("domain") != "firoyang.com" {
		reply("-1", "bad token", nil)
		return
	}
	switch action {
	case "Record.Create":
		f.nextID++
		id := json.Number(itoa(f.nextID))
		rec := dnspodRecord{ID: id, Name: r.Form.Get("sub_domain"), Type: r.Form.Get("record_type"), Value: r.Form.Get("value")}
		f.records[string(id)] = rec
		reply("1", "ok", map[string]any{"record": map[string]any{"id": string(id)}})
	case "Record.Remove":
		delete(f.records, r.Form.Get("record_id"))
		reply("1", "ok", nil)
	case "Record.Modify":
		id := r.Form.Get("record_id")
		rec := f.records[id]
		rec.Value = r.Form.Get("value")
		f.records[id] = rec
		reply("1", "ok", nil)
	case "Record.List":
		var out []dnspodRecord
		for _, rec := range f.records {
			if rec.Name == r.Form.Get("sub_domain") && rec.Type == r.Form.Get("record_type") {
				out = append(out, rec)
			}
		}
		if len(out) == 0 {
			reply("10", "记录列表为空", nil)
			return
		}
		reply("1", "ok", map[string]any{"records": out})
	default:
		reply("-1", "unknown", nil)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func newTestDNSPod(t *testing.T) (*DNSPod, *fakeDNSPod) {
	f := newFakeDNSPod()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &DNSPod{Token: f.token, Zone: "firoyang.com", Endpoint: srv.URL, HTTP: srv.Client()}, f
}

func TestDNSPodPresentAndCleanup(t *testing.T) {
	d, f := newTestDNSPod(t)
	cleanup, err := d.Present(context.Background(), "_acme-challenge.local.dec.firoyang.com", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.records) != 1 {
		t.Fatalf("应创建 1 条记录: %v", f.records)
	}
	for _, rec := range f.records {
		if rec.Name != "_acme-challenge.local.dec" || rec.Type != "TXT" || rec.Value != "tok" {
			t.Fatalf("记录内容错误: %+v", rec)
		}
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.records) != 0 {
		t.Fatal("清理后应删除记录")
	}
}

func TestDNSPodEnsureAIdempotent(t *testing.T) {
	d, f := newTestDNSPod(t)
	ctx := context.Background()
	changed, err := d.EnsureA(ctx, "local.dec.firoyang.com", "127.0.0.1")
	if err != nil || !changed {
		t.Fatalf("首次应创建: %v %v", changed, err)
	}
	changed, err = d.EnsureA(ctx, "local.dec.firoyang.com", "127.0.0.1")
	if err != nil || changed {
		t.Fatalf("已正确时不应修改: %v %v", changed, err)
	}

	// 多条且值不对：改第一条、删其余。
	f.records["900"] = dnspodRecord{ID: "900", Name: "local.dec", Type: "A", Value: "1.2.3.4"}
	changed, err = d.EnsureA(ctx, "local.dec.firoyang.com", "127.0.0.1")
	if err != nil || !changed {
		t.Fatalf("应收敛: %v %v", changed, err)
	}
	var as []dnspodRecord
	for _, r := range f.records {
		if r.Type == "A" {
			as = append(as, r)
		}
	}
	if len(as) != 1 || as[0].Value != "127.0.0.1" {
		t.Fatalf("收敛后应只有一条 127.0.0.1: %+v", as)
	}
}

func TestDNSPodErrors(t *testing.T) {
	d, _ := newTestDNSPod(t)
	if _, err := d.Present(context.Background(), "x.example.com", "v"); err == nil {
		t.Fatal("非本主域名应拒绝")
	}
	d.Token = "wrong"
	if _, err := d.Present(context.Background(), "_acme-challenge.local.dec.firoyang.com", "v"); err == nil {
		t.Fatal("token 错误应报错")
	}
	d.Token = ""
	if _, err := d.EnsureA(context.Background(), "local.dec.firoyang.com", "127.0.0.1"); err != ErrNotConfigured {
		t.Fatalf("未配置 token 应返回 ErrNotConfigured: %v", err)
	}
}

func TestSubDomain(t *testing.T) {
	d := &DNSPod{Zone: "firoyang.com."}
	cases := map[string]string{
		"firoyang.com":                            "@",
		"local.dec.firoyang.com":                  "local.dec",
		"_acme-challenge.local.dec.firoyang.com.": "_acme-challenge.local.dec",
	}
	for in, want := range cases {
		got, err := d.subDomain(in)
		if err != nil || got != want {
			t.Errorf("subDomain(%q) = %q, %v，期望 %q", in, got, err, want)
		}
	}
	if _, err := d.subDomain("evilfiroyang.com"); err == nil {
		t.Error("evilfiroyang.com 不应被视为子域")
	}
}
