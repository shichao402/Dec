package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWaitForGatewayUsesConsoleJSON(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/agent/hello" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(Hello{Version: "test", Connected: true, Unlocked: true})
	}))
	t.Cleanup(srv.Close)

	endpoint := strings.TrimPrefix(srv.URL, "http://")
	path, err := ConsoleMetadataPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(consoleMetadata{Version: 1, Endpoint: endpoint, Token: "tok", PID: 1})
	if err := os.WriteFile(path, meta, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	gw, err := WaitForGateway(ctx, "mcp-test")
	if err != nil {
		t.Fatal(err)
	}
	hello, err := gw.Hello(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hello.Connected {
		t.Fatalf("%#v", hello)
	}
}

func TestWaitForGatewayDoesNotLaunchInTests(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_, err := WaitForGateway(ctx, "mcp-test")
	if err == nil {
		t.Fatal("缺 console.json 时测试进程不得假装成功")
	}
	if !strings.Contains(err.Error(), "当前环境不可启动") && !strings.Contains(err.Error(), "拉起失败") {
		t.Fatalf("got %v", err)
	}
}

func TestRefreshingGatewayFollowsConsoleJSONEndpointChange(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())

	var hits struct {
		sync.Mutex
		old, neu int
	}
	oldSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Lock()
		hits.old++
		hits.Unlock()
		if r.Header.Get("Authorization") != "Bearer old-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"from": "old"},
		})
	}))
	t.Cleanup(oldSrv.Close)

	newSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Lock()
		hits.neu++
		hits.Unlock()
		if r.Header.Get("Authorization") != "Bearer new-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"from": "new"},
		})
	}))
	t.Cleanup(newSrv.Close)

	path, err := ConsoleMetadataPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeMeta := func(endpoint, token string, pid int) {
		t.Helper()
		meta, _ := json.Marshal(consoleMetadata{Version: 1, Endpoint: endpoint, Token: token, PID: pid})
		if err := os.WriteFile(path, meta, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeMeta(strings.TrimPrefix(oldSrv.URL, "http://"), "old-tok", 1)

	gw, err := newRefreshingGateway("mcp-test")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := gw.ToolCall(ctx, "dec_status", json.RawMessage(`{}`), "test")
	if err != nil || res == nil || !res.OK {
		t.Fatalf("first call: res=%#v err=%v", res, err)
	}
	var first map[string]any
	_ = json.Unmarshal(res.Result, &first)
	if first["from"] != "old" {
		t.Fatalf("first result = %#v", first)
	}

	writeMeta(strings.TrimPrefix(newSrv.URL, "http://"), "new-tok", 2)
	res, err = gw.ToolCall(ctx, "dec_status", json.RawMessage(`{}`), "test")
	if err != nil || res == nil || !res.OK {
		t.Fatalf("second call: res=%#v err=%v", res, err)
	}
	var second map[string]any
	_ = json.Unmarshal(res.Result, &second)
	if second["from"] != "new" {
		t.Fatalf("second result = %#v", second)
	}
	hits.Lock()
	defer hits.Unlock()
	if hits.old < 1 || hits.neu < 1 {
		t.Fatalf("hits old=%d new=%d", hits.old, hits.neu)
	}
}

func TestRefreshingGatewayRetriesAfterStaleEndpoint(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())

	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer live-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"from": "live"},
		})
	}))
	t.Cleanup(live.Close)

	path, err := ConsoleMetadataPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	liveEndpoint := strings.TrimPrefix(live.URL, "http://")
	meta, _ := json.Marshal(consoleMetadata{Version: 1, Endpoint: liveEndpoint, Token: "live-tok", PID: 2})
	if err := os.WriteFile(path, meta, 0o600); err != nil {
		t.Fatal(err)
	}
	gw, err := newRefreshingGateway("mcp-test")
	if err != nil {
		t.Fatal(err)
	}

	// Poison the cached client to a closed listener while leaving meta matching
	// console.json, so ensure(false) keeps the stale transport until dial fails.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	stale, err := gatewayFromMeta(&consoleMetadata{
		Version:  1,
		Endpoint: strings.TrimPrefix(deadURL, "http://"),
		Token:    "dead-tok",
		PID:      1,
	}, "mcp-test")
	if err != nil {
		t.Fatal(err)
	}
	gw.mu.Lock()
	gw.inner = stale
	gw.mu.Unlock()

	res, err := gw.ToolCall(context.Background(), "dec_status", json.RawMessage(`{}`), "test")
	if err != nil || res == nil || !res.OK {
		t.Fatalf("retry after refresh: res=%#v err=%v", res, err)
	}
	var body map[string]any
	_ = json.Unmarshal(res.Result, &body)
	if body["from"] != "live" {
		t.Fatalf("body = %#v", body)
	}
}

func TestShouldRefreshGateway(t *testing.T) {
	cases := []struct {
		err  string
		want bool
	}{
		{"Post \"http://127.0.0.1:4010/agent/tool_call\": dial tcp 127.0.0.1:4010: connectex: No connection could be made because the target machine actively refused it.", true},
		{"Console 网关 HTTP 401", true},
		{"something unrelated", false},
	}
	for _, tc := range cases {
		got := shouldRefreshGateway(fmt.Errorf("%s", tc.err))
		if got != tc.want {
			t.Fatalf("%q -> %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestConsoleMetadataStale(t *testing.T) {
	// PID<=1 无从判断，一律不判 stale（兼容未写 PID 的老文件与测试 fixture）。
	for _, pid := range []int{0, 1, -1} {
		if consoleMetadataStale(&consoleMetadata{Version: 1, Endpoint: "127.0.0.1:1", Token: "t", PID: pid}) {
			t.Fatalf("PID=%d 不应判 stale", pid)
		}
	}
	if consoleMetadataStale(nil) {
		t.Fatal("nil 不应判 stale")
	}
	// 一个几乎不可能存在的 PID 必须判为残留。
	if !consoleMetadataStale(&consoleMetadata{Version: 1, Endpoint: "127.0.0.1:1", Token: "t", PID: 0x7FFF_FFFE}) {
		t.Fatal("已死 PID 应判 stale")
	}
}

// TestWithInnerReportsStaleConsoleWhenProcessGone 锁定：Console 退出后残留的
// console.json 会让两次调用都 refused；此时应当如实报「Console 未运行」，
// 而不是抛回一个指向旧端口的 connection refused。
//
// 注意：stale 只在「调用确实失败」之后才判定，绝不用来阻断调用——
// PID 会被系统复用，拿到一个活着的 PID 并不等于 endpoint 可用。
func TestWithInnerReportsStaleConsoleWhenProcessGone(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	path, err := ConsoleMetadataPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// 指向一个确定无人监听的端口，并配一个已死的 PID。
	meta, _ := json.Marshal(consoleMetadata{
		Version:  1,
		Endpoint: "127.0.0.1:1",
		Token:    "tok",
		PID:      0x7FFF_FFFE,
	})
	if err := os.WriteFile(path, meta, 0o600); err != nil {
		t.Fatal(err)
	}

	g, err := newRefreshingGateway("mcp-test")
	if err != nil {
		t.Fatal(err)
	}
	err = g.withInner(context.Background(), func(*httpGateway) error {
		return fmt.Errorf("connectex: No connection could be made because the target machine actively refused it")
	})
	if err == nil {
		t.Fatal("调用失败且 Console 进程已死时必须报错")
	}
	var stale *staleEndpointError
	if !errors.As(err, &stale) {
		t.Fatalf("want *staleEndpointError, got %T: %v", err, stale)
	}
	if !strings.Contains(err.Error(), "Dec Console 未运行") {
		t.Fatalf("err = %v", err)
	}
}

// TestWithInnerKeepsOriginalErrorWhenProcessAlive 锁定：PID 仍活着时不得把
// 真实失败改写为 stale —— 那时问题在别处（token 轮换、路径变更等），
// 掩盖原始错误会让排查更难。
func TestWithInnerKeepsOriginalErrorWhenProcessAlive(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	path, err := ConsoleMetadataPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	currentPID := os.Getpid()
	meta, _ := json.Marshal(consoleMetadata{
		Version:  1,
		Endpoint: "127.0.0.1:1",
		Token:    "tok",
		PID:      currentPID,
	})
	if err := os.WriteFile(path, meta, 0o600); err != nil {
		t.Fatal(err)
	}

	g, err := newRefreshingGateway("mcp-test")
	if err != nil {
		t.Fatal(err)
	}
	const want = "connectex: refused"
	err = g.withInner(context.Background(), func(*httpGateway) error {
		return fmt.Errorf("%s", want)
	})
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("PID 存活时应保留原始错误，got %v", err)
	}
	var stale *staleEndpointError
	if errors.As(err, &stale) {
		t.Fatalf("PID 存活时不得改写为 stale: %v", err)
	}
}
