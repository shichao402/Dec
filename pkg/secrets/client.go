package secrets

import (
	"context"
)

// Client 拉取 Bitwarden secrets bundle 的 API 抽象。
type Client interface {
	PullBundle(ctx context.Context, req PullBundleRequest) (*PullBundleResult, error)
}

// StubClient 测试/开发用 stub；按 Bitwarden folder 返回预设 Note。
type StubClient struct {
	NotesByFolder map[string][]SecureNote
}

func (c *StubClient) PullBundle(_ context.Context, req PullBundleRequest) (*PullBundleResult, error) {
	folder := req.Binding.BitwardenFolder
	if folder == "" {
		folder = req.Binding.SecretsBundleName
	}
	if folder == "" {
		folder = req.DecBundleName
	}
	notes := c.NotesByFolder[folder]
	if notes == nil {
		return &PullBundleResult{}, nil
	}
	copied := make([]SecureNote, len(notes))
	copy(copied, notes)
	return &PullBundleResult{Notes: copied}, nil
}

// NoopClient 未配置 Bitwarden API 时的空实现。
type NoopClient struct{}

func (NoopClient) PullBundle(_ context.Context, _ PullBundleRequest) (*PullBundleResult, error) {
	return &PullBundleResult{}, nil
}

// DefaultClient 在已配置且有 session 时返回真实 APIClient，否则 NoopClient。
func DefaultClient() Client {
	if !HasSession() {
		return NoopClient{}
	}
	configured, err := IsConfigured()
	if err != nil || !configured {
		return NoopClient{}
	}
	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		return NoopClient{}
	}
	client, err := NewAPIClient(cfg, Session(), httpClientFactory())
	if err != nil {
		return NoopClient{}
	}
	return client
}
