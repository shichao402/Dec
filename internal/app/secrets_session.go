package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/secrets"
)

func ensureBitwardenSession(ctx context.Context, reporter Reporter, phase string) error {
	if secrets.HasSession() {
		emit(reporter, EventInfo, phase, "[auth] session check: hasSession=true", nil)
		return nil
	}
	emit(reporter, EventInfo, phase, "[auth] Bitwarden session required, starting unlock", nil)
	sessionOpts := &secrets.EnsureSessionOpts{
		OnStatus: func(message string) {
			level := EventInfo
			if strings.Contains(message, "failed:") ||
				strings.Contains(message, "timeout") ||
				strings.Contains(message, "timeout -") {
				level = EventWarn
			}
			emit(reporter, level, phase, message, nil)
		},
		OnUnlockURL: func(url string) {
			emit(reporter, EventInfo, phase, fmt.Sprintf("[auth] web unlock: unlock page %s", url), nil)
			emit(reporter, EventInfo, phase, "若浏览器未自动打开，请复制上方链接到浏览器访问", nil)
		},
		OnBrowserError: func(err error) {
			emit(reporter, EventWarn, phase, fmt.Sprintf("[auth] web unlock: cannot open browser: %v", err), nil)
		},
	}
	if unlockCfg := unlockConfigFromContext(ctx); unlockCfg.Timeout > 0 {
		sessionOpts.UnlockTimeout = unlockCfg.Timeout
	}
	if err := secrets.EnsureSession(ctx, sessionOpts); err != nil {
		emit(reporter, EventWarn, phase, fmt.Sprintf("[auth] unlock failed: %v", err), nil)
		return fmt.Errorf("Bitwarden 解锁失败: %w", err)
	}
	emit(reporter, EventInfo, phase, "[auth] session ready", nil)
	return nil
}
