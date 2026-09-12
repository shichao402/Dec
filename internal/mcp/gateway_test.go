package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
