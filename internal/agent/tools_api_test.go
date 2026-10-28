package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeLegacy struct {
	scanErr  error
	executed []string
	restored []string
	deleted  []string
}

func (f *fakeLegacy) tools() *LegacyTools {
	return &LegacyTools{
		Scan: func() (any, error) {
			if f.scanErr != nil {
				return nil, f.scanErr
			}
			return map[string]any{"items": []any{}, "fingerprint": "fp"}, nil
		},
		Execute: func(fp string) (any, error) {
			f.executed = append(f.executed, fp)
			return map[string]any{"empty": true}, nil
		},
		Archives: func() (any, error) { return []string{"20261028T000000Z"}, nil },
		Archive:  func(id string) (any, error) { return map[string]string{"id": id}, nil },
		Restore: func(id string, idx []int) (any, error) {
			f.restored = append(f.restored, id)
			return map[string]any{"id": id, "n": len(idx)}, nil
		},
		DeleteArchive: func(id string) error {
			f.deleted = append(f.deleted, id)
			return nil
		},
	}
}

func toolsLoopback(f *fakeLegacy) (http.Handler, func(method, path, ctype, body string, hdr ...string) *httptest.ResponseRecorder) {
	l := NewLoopback(LoopbackOptions{Legacy: f.tools()})
	h := l.Handler()
	l.addr = "127.0.0.1:2548"
	do := func(method, path, ctype, body string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:2548"+path, strings.NewReader(body))
		req.Host = "127.0.0.1:2548"
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	return h, do
}

// 只读接口只收 GET；扫描失败返回 409 并带原因（需求 3.1）。
func TestLegacyToolsReadEndpoints(t *testing.T) {
	f := &fakeLegacy{}
	_, do := toolsLoopback(f)
	for _, p := range []string{toolLegacyScan, toolLegacyArchives, toolLegacyArchive + "?id=x"} {
		if rec := do(http.MethodPost, p, "application/json", "{}"); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s POST 应被拒，得到 %d", p, rec.Code)
		}
		if rec := do(http.MethodGet, p, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("%s GET 应成功，得到 %d %s", p, rec.Code, rec.Body)
		}
	}
	if rec := do(http.MethodGet, toolLegacyArchive+"?id=abc", "", ""); !strings.Contains(rec.Body.String(), `"abc"`) {
		t.Fatalf("应按 id 读归档：%s", rec.Body)
	}
	f.scanErr = errors.New("还没有和云端完成过一次对账")
	rec := do(http.MethodGet, toolLegacyScan, "", "")
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusConflict || !strings.Contains(body["error"], "对账") {
		t.Fatalf("扫描失败应 409 并带原因：%d %v", rec.Code, body)
	}
}

// 会动文件的接口只收 POST + application/json，外部 Origin、外部 Host 都进不来（需求 1.2）。
func TestLegacyToolsWriteEndpointsGuarded(t *testing.T) {
	f := &fakeLegacy{}
	_, do := toolsLoopback(f)
	writes := map[string]string{
		toolLegacyExecute: `{"fingerprint":"fp"}`,
		toolLegacyRestore: `{"id":"a","indexes":[1]}`,
		toolLegacyDelete:  `{"id":"a"}`,
	}
	for p, body := range writes {
		if rec := do(http.MethodGet, p, "", ""); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s GET 应被拒，得到 %d", p, rec.Code)
		}
		// 跨站表单能发出的类型。
		for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
			if rec := do(http.MethodPost, p, ct, body); rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("%s 以 %q 提交应被拒，得到 %d", p, ct, rec.Code)
			}
		}
		if rec := do(http.MethodPost, p, "application/json", body, "Origin", "https://evil.example.com"); rec.Code != http.StatusForbidden {
			t.Fatalf("%s 外部 Origin 应被拒，得到 %d", p, rec.Code)
		}
		if rec := do(http.MethodPost, p, "application/json", "{bad"); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 坏 JSON 应 400，得到 %d", p, rec.Code)
		}
	}
	if len(f.executed)+len(f.restored)+len(f.deleted) != 0 {
		t.Fatal("被拒的请求不应触发任何操作")
	}
	for p, body := range writes {
		if rec := do(http.MethodPost, p, "application/json; charset=utf-8", body, "Origin", "http://127.0.0.1:2548"); rec.Code != http.StatusOK {
			t.Fatalf("%s 应成功，得到 %d %s", p, rec.Code, rec.Body)
		}
	}
	if strings.Join(f.executed, ",") != "fp" || strings.Join(f.restored, ",") != "a" || strings.Join(f.deleted, ",") != "a" {
		t.Fatalf("参数没有原样传下去：%v %v %v", f.executed, f.restored, f.deleted)
	}
}

// 未装配时返回 501，不 panic。
func TestLegacyToolsDisabled(t *testing.T) {
	l := NewLoopback(LoopbackOptions{})
	l.addr = "127.0.0.1:2548"
	for _, c := range []struct{ m, p, ct string }{
		{http.MethodGet, toolLegacyScan, ""},
		{http.MethodPost, toolLegacyExecute, "application/json"},
	} {
		req := httptest.NewRequest(c.m, "http://127.0.0.1:2548"+c.p, strings.NewReader("{}"))
		req.Host = "127.0.0.1:2548"
		if c.ct != "" {
			req.Header.Set("Content-Type", c.ct)
		}
		rec := httptest.NewRecorder()
		l.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("%s 未装配应 501，得到 %d", c.p, rec.Code)
		}
	}
}
