package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 扫描接口只读：只接受 GET，扫描失败（例如还没对账）返回 409 与原因。
func TestLegacyScanEndpoint(t *testing.T) {
	calls := 0
	fail := false
	l := NewLoopback(LoopbackOptions{
		LegacyScan: func() (any, error) {
			calls++
			if fail {
				return nil, errors.New("还没有和云端完成过一次对账")
			}
			return map[string]int{"items": 0}, nil
		},
	})
	h := l.Handler()
	l.addr = "127.0.0.1:4100"

	do := func(method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:4100/api/tools/legacy/scan", nil)
		req.Host = "127.0.0.1:4100"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := do(http.MethodPost); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST 应被拒，实际 %d", rec.Code)
	}
	if calls != 0 {
		t.Fatal("被拒的请求不应触发扫描")
	}
	if rec := do(http.MethodGet); rec.Code != http.StatusOK {
		t.Fatalf("GET 应成功，实际 %d %s", rec.Code, rec.Body.String())
	}
	fail = true
	rec := do(http.MethodGet)
	if rec.Code != http.StatusConflict {
		t.Fatalf("扫描失败应返回 409，实际 %d", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !strings.Contains(body["error"], "对账") {
		t.Fatalf("应带上失败原因，实际 %v", body)
	}
}
