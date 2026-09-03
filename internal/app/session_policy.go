package app

import (
	"context"
	"time"
)

type unlockConfigKey struct{}

// UnlockConfig controls how an app operation may obtain a Bitwarden session.
type UnlockConfig struct {
	// Timeout is the maximum wait for Console Authenticate.
	Timeout          time.Duration
	InteractiveLocal bool
	Facade           string
	ClientID         string
	Operation        string
	OperationID      string
	ProjectRoot      string
	WorkspacePlane   string
}

// MCPSessionUnlockTimeout is the maximum wait for a local MCP-triggered
// Console authentication.
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
