//go:build live

package secrets

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	root := FindProjectRootWithIntegrationAuth()
	if root != "" {
		if _, err := ApplyIntegrationAuth(root); err != nil {
			panic("ApplyIntegrationAuth: " + err.Error())
		}
	}
	os.Exit(m.Run())
}

func requireLivePassword(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("DEC_BW_PASSWORD")) == "" {
		t.Skip("未找到 DEC_BW_PASSWORD 或 .secrets/dec/integration/bitwarden.yaml")
	}
}

// 运行: go test -tags live ./internal/secrets -run TestLive_EnsureSession -v
func TestLive_EnsureSession(t *testing.T) {
	requireLivePassword(t)
	ClearSession()
	t.Cleanup(ClearSession)

	var statuses []string
	if err := EnsureSession(context.Background(), &EnsureSessionOpts{
		OnStatus: func(message string) {
			statuses = append(statuses, message)
			t.Logf("status: %s", message)
		},
	}); err != nil {
		t.Fatalf("EnsureSession() = %v", err)
	}
	if !HasSession() {
		t.Fatal("应有 session")
	}
	if !HasUserKey() {
		t.Fatal("应有 vault user key")
	}
	found := false
	for _, s := range statuses {
		if strings.Contains(s, "programmatic unlock: success") || strings.Contains(s, "session ready") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("期望程序化解锁成功状态，实际: %v", statuses)
	}
}

// 运行: go test -tags live ./internal/secrets -run TestLive_EnsureSession_NoPassword -v
func TestLive_EnsureSession_NoPassword(t *testing.T) {
	if os.Getenv("DEC_BW_PASSWORD") != "" {
		t.Setenv("DEC_BW_PASSWORD", "")
	}
	ClearSession()
	t.Cleanup(ClearSession)

	var statuses []string
	err := EnsureSession(context.Background(), &EnsureSessionOpts{
		OnStatus: func(message string) {
			statuses = append(statuses, message)
		},
	})
	if err == nil {
		t.Fatal("无 DEC_BW_PASSWORD 时应要求 Console 解锁")
	}
	if !errors.Is(err, ErrConsoleUnlockRequired) {
		t.Fatalf("EnsureSession() = %v", err)
	}
	found := false
	for _, s := range statuses {
		if strings.Contains(s, "programmatic unlock: skipped (DEC_BW_PASSWORD not set)") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("期望未设置密码提示，实际: %v", statuses)
	}
}
