//go:build live

package secrets

import (
	"context"
	"os"
	"testing"

	"github.com/shichao402/Dec/pkg/secrets/unlock"
)

// 运行: DEC_BW_PASSWORD='...' go test -tags live ./pkg/secrets -run TestLive_EnsureSession -v
func TestLive_EnsureSession(t *testing.T) {
	password := os.Getenv("DEC_BW_PASSWORD")
	if password == "" {
		t.Skip("设置 DEC_BW_PASSWORD 以运行 live EnsureSession 测试")
	}
	ClearSession()
	t.Cleanup(ClearSession)

	webUnlockCalled := false
	origRun := unlockRun
	unlockRun = func(ctx context.Context, opts unlock.Options) error {
		webUnlockCalled = true
		t.Fatal("DEC_BW_PASSWORD 已设置且 remember token 有效时不应触发 web unlock")
		return nil
	}
	t.Cleanup(func() { unlockRun = origRun })

	var statuses []string
	if err := EnsureSession(context.Background(), &EnsureSessionOpts{
		OnStatus: func(message string) {
			statuses = append(statuses, message)
			t.Logf("status: %s", message)
		},
	}); err != nil {
		t.Fatalf("EnsureSession() = %v", err)
	}
	if webUnlockCalled {
		t.Fatal("web unlock 不应被调用")
	}
	if !HasSession() {
		t.Fatal("应有 session")
	}
	if !HasUserKey() {
		t.Fatal("应有 vault user key")
	}
	found := false
	for _, s := range statuses {
		if s == "Bitwarden 已通过程序化登录解锁" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("期望程序化解锁成功状态，实际: %v", statuses)
	}
}

// 运行: go test -tags live ./pkg/secrets -run TestLive_EnsureSession_NoPassword -v
func TestLive_EnsureSession_NoPassword(t *testing.T) {
	if os.Getenv("DEC_BW_PASSWORD") != "" {
		t.Setenv("DEC_BW_PASSWORD", "")
	}
	ClearSession()
	t.Cleanup(ClearSession)

	webUnlockCalled := false
	origRun := unlockRun
	unlockRun = func(ctx context.Context, opts unlock.Options) error {
		webUnlockCalled = true
		return context.Canceled // 不阻塞 live 测试
	}
	t.Cleanup(func() { unlockRun = origRun })

	var statuses []string
	err := EnsureSession(context.Background(), &EnsureSessionOpts{
		OnStatus: func(message string) {
			statuses = append(statuses, message)
		},
	})
	if err == nil {
		t.Fatal("无 DEC_BW_PASSWORD 时应进入 web unlock 并被取消")
	}
	if !webUnlockCalled {
		t.Fatal("无 DEC_BW_PASSWORD 时应回退 web unlock")
	}
	found := false
	for _, s := range statuses {
		if s == "未设置 DEC_BW_PASSWORD，将使用 web unlock" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("期望未设置密码提示，实际: %v", statuses)
	}
}
