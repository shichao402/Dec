package app

import (
	"context"
	"time"
)

type unlockConfigKey struct{}

// UnlockConfig 控制 app 操作链中的 Bitwarden web unlock 超时。
type UnlockConfig struct {
	// Timeout 为 web unlock 等待时长；零值使用 secrets/unlock 默认（5 分钟）。
	Timeout        time.Duration
	Facade         string
	ClientID       string
	Operation      string
	OperationID    string
	ProjectRoot    string
	WorkspacePlane string
}

// MCPSessionUnlockTimeout 是 MCP tool 调用的 web unlock 超时。
const MCPSessionUnlockTimeout = 3 * time.Minute

// WithUnlockConfig 将解锁配置附加到 context，供 ensureBitwardenSession 读取。
func WithUnlockConfig(ctx context.Context, cfg UnlockConfig) context.Context {
	return context.WithValue(ctx, unlockConfigKey{}, cfg)
}

func unlockConfigFromContext(ctx context.Context) UnlockConfig {
	if v, ok := ctx.Value(unlockConfigKey{}).(UnlockConfig); ok {
		return v
	}
	return UnlockConfig{}
}
