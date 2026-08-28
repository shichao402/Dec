package app

import (
	"context"
	"fmt"

	"github.com/shichao402/Dec/internal/secrets"
)

func ensureBitwardenSession(ctx context.Context, reporter Reporter, phase string) error {
	if secrets.HasSession() {
		return nil
	}
	emit(reporter, EventInfo, phase, "Bitwarden 未解锁，正在解锁…", nil)
	sessionOpts := &secrets.EnsureSessionOpts{
		RequestSource: phase,
		OnUnlockURL: func(url string) {
			emit(reporter, EventInfo, phase, fmt.Sprintf("解锁页: %s", url), nil)
		},
		OnBrowserError: func(err error) {
			emit(reporter, EventWarn, phase, fmt.Sprintf("无法自动打开浏览器，请手动访问解锁页: %v", err), nil)
		},
	}
	unlockCfg := unlockConfigFromContext(ctx)
	sessionOpts.UnlockTimeout = unlockCfg.Timeout
	sessionOpts.Facade = unlockCfg.Facade
	sessionOpts.ClientID = unlockCfg.ClientID
	sessionOpts.Operation = unlockCfg.Operation
	sessionOpts.OperationID = unlockCfg.OperationID
	sessionOpts.ProjectRoot = unlockCfg.ProjectRoot
	sessionOpts.WorkspacePlane = unlockCfg.WorkspacePlane
	if err := secrets.EnsureSession(ctx, sessionOpts); err != nil {
		emit(reporter, EventWarn, phase, fmt.Sprintf("Bitwarden 解锁失败: %v", err), nil)
		return fmt.Errorf("Bitwarden 解锁失败: %w", err)
	}
	emit(reporter, EventInfo, phase, "Bitwarden 已解锁", nil)
	return nil
}
