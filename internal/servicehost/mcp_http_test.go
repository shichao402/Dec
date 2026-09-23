package servicehost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMCPHTTPRejectsNonLoopback(t *testing.T) {
	host := &Server{
		version:  "test",
		presence: newPresenceTracker(time.Hour, func() {}),
		broker:   newOperationBroker(),
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	newMCPHTTPServer(host).Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestMCPHTTPAcceptsLoopbackWithoutBearer(t *testing.T) {
	host := &Server{
		version:  "test",
		presence: newPresenceTracker(time.Hour, func() {}),
		broker:   newOperationBroker(),
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`))
	req.RemoteAddr = "127.0.0.1:4321"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	newMCPHTTPServer(host).Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
