package acceptance

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsAgentLog(t *testing.T) {
	cases := map[string]bool{
		"agent.log":     true,
		"agent.log.1":   true,
		"agent.log.12":  true,
		"agent.log.":    false,
		"agent.log.bak": false,
		"agent.err.log": false,
		"tray.log":      false,
	}
	for name, want := range cases {
		if got := isAgentLog(name); got != want {
			t.Errorf("isAgentLog(%q)=%v，期望 %v", name, got, want)
		}
	}
}

func TestCheckSingleLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent.log")
	legacy := filepath.Join(dir, "agent.err.log")
	for _, p := range []string{logPath, legacy} {
		if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// 只有 agent.log 变化：通过，并提示旧文件可删。
	r := &runner{opts: Options{Log: io.Discard}, res: &Result{}, logPath: logPath, logSnap: snapshotLogs(logPath)}
	appendFile(t, logPath)
	r.checkSingleLog()
	if s := r.res.Steps[0]; !s.Passed || !strings.Contains(s.Detail, "agent.err.log") {
		t.Fatalf("只写 agent.log 应通过并提示旧文件：%+v", s)
	}

	// 旧文件也在变化：失败。
	r = &runner{opts: Options{Log: io.Discard}, res: &Result{}, logPath: logPath, logSnap: snapshotLogs(logPath)}
	appendFile(t, legacy)
	r.checkSingleLog()
	if s := r.res.Steps[0]; s.Passed || !strings.Contains(s.Detail, "agent.err.log") {
		t.Fatalf("旧文件被写入应失败：%+v", s)
	}
}

func appendFile(t *testing.T, p string) {
	t.Helper()
	// 拉开修改时间，避免文件系统时间精度掩盖变化。
	time.Sleep(10 * time.Millisecond)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("more\n"); err != nil {
		t.Fatal(err)
	}
}

// TestCheckDotLocalhostPinsLoopback 验证请求钉到实际监听地址，且 Host 头是 dec.localhost。
func TestCheckDotLocalhostPinsLoopback(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		_, _ = io.WriteString(w, `{"device_id":"dev-1"}`)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	r := &runner{opts: Options{Log: io.Discard}, res: &Result{}}
	r.checkDotLocalhost(context.Background(), accessStatus{DeviceID: "dev-1", HTTPAddr: addr})
	if s := r.res.Steps[0]; !s.Passed {
		t.Fatalf("应通过：%+v", s)
	}
	if !strings.HasPrefix(gotHost, "dec.localhost:") {
		t.Fatalf("Host 头应为 dec.localhost:<端口>，得到 %q", gotHost)
	}

	// device_id 不一致说明端口上是别的进程。
	r = &runner{opts: Options{Log: io.Discard}, res: &Result{}}
	r.checkDotLocalhost(context.Background(), accessStatus{DeviceID: "other", HTTPAddr: addr})
	if r.res.Steps[0].Passed {
		t.Fatal("device_id 不一致应失败")
	}
}

func TestCheckAdminURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"device_id":"dev-1"}`)
	}))
	defer srv.Close()

	r := &runner{opts: Options{Log: io.Discard}, res: &Result{}}
	r.checkAdminURL(context.Background(), accessStatus{DeviceID: "dev-1", AdminURL: srv.URL + "/admin"})
	if s := r.res.Steps[0]; !s.Passed {
		t.Fatalf("应通过：%+v", s)
	}

	for _, admin := range []string{"https://local.dec.firoyang.com/admin", "http://dec.localhost:1/admin", ""} {
		r = &runner{opts: Options{Log: io.Discard}, res: &Result{}}
		r.checkAdminURL(context.Background(), accessStatus{DeviceID: "dev-1", AdminURL: admin})
		if r.res.Steps[0].Passed {
			t.Fatalf("admin_url=%q 不是回环地址，应失败", admin)
		}
	}
}
