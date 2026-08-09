package secrets

import (
	"context"
	"strings"
)

// Client 拉取/推送 Bitwarden secrets bundle 的 API 抽象。
type Client interface {
	PullBundle(ctx context.Context, req PullBundleRequest) (*PullBundleResult, error)
	PushBundle(ctx context.Context, req PushBundleRequest, notes []SecureNote) (*PushBundleResult, error)
	DeleteSecureNote(ctx context.Context, req DeleteSecureNoteRequest) error
	DeleteSSHKey(ctx context.Context, req DeleteSSHKeyRequest) error
	// ListFolderNotes 枚举 folder 下的 note 名（相对 SyncTarget.LocalRoot）。
	ListFolderNotes(ctx context.Context, folderName string) ([]RemoteNote, error)
	// ListFolderSSHKeys 枚举 folder 下的 SSH Key 逻辑名。
	ListFolderSSHKeys(ctx context.Context, folderName string) ([]RemoteSSHKey, error)
}

// StubClient 测试/开发用 stub；按 Bitwarden folder 返回预设 Note / SSH Key。
type StubClient struct {
	NotesByFolder   map[string][]SecureNote
	SSHKeysByFolder map[string][]SSHKeyItem
}

func stubFolder(reqFolder, bindingFolder, decBundleName string) string {
	if name := strings.TrimSpace(reqFolder); name != "" {
		return name
	}
	if name := strings.TrimSpace(bindingFolder); name != "" {
		return name
	}
	return DefaultBundleFolder(decBundleName)
}

func (c *StubClient) PullBundle(_ context.Context, req PullBundleRequest) (*PullBundleResult, error) {
	folder := stubFolder(req.Target.Folder, req.Binding.SecretsBundleName, req.DecBundleName)
	notes := c.NotesByFolder[folder]
	keys := c.SSHKeysByFolder[folder]
	result := &PullBundleResult{}
	if notes != nil {
		copied := make([]SecureNote, len(notes))
		copy(copied, notes)
		result.Notes = copied
	}
	if keys != nil {
		copied := make([]SSHKeyItem, len(keys))
		copy(copied, keys)
		result.SSHKeys = copied
	}
	return result, nil
}

func (c *StubClient) PushBundle(_ context.Context, req PushBundleRequest, notes []SecureNote) (*PushBundleResult, error) {
	folder := stubFolder(req.Target.Folder, req.Binding.SecretsBundleName, req.DecBundleName)
	if c.NotesByFolder == nil {
		c.NotesByFolder = make(map[string][]SecureNote)
	}
	// 与 APIClient 一致：只 create/update，不删除本次未覆盖的远端 note。
	merged := append([]SecureNote(nil), c.NotesByFolder[folder]...)
	indexOf := make(map[string]int, len(merged))
	for i, note := range merged {
		indexOf[note.RelativePath] = i
	}

	result := &PushBundleResult{}
	for _, note := range notes {
		if i, ok := indexOf[note.RelativePath]; ok {
			merged[i] = note
			result.Updated++
		} else {
			indexOf[note.RelativePath] = len(merged)
			merged = append(merged, note)
			result.Created++
		}
		result.Paths = append(result.Paths, note.RelativePath)
	}
	c.NotesByFolder[folder] = merged
	return result, nil
}

func (c *StubClient) DeleteSecureNote(_ context.Context, req DeleteSecureNoteRequest) error {
	folder := stubFolder(req.Target.Folder, req.Binding.SecretsBundleName, "")
	notes := c.NotesByFolder[folder]
	if len(notes) == 0 {
		return nil
	}
	out := make([]SecureNote, 0, len(notes))
	for _, note := range notes {
		if note.RelativePath != req.NotePath {
			out = append(out, note)
		}
	}
	c.NotesByFolder[folder] = out
	return nil
}

func (c *StubClient) DeleteSSHKey(_ context.Context, req DeleteSSHKeyRequest) error {
	folder := stubFolder(req.Target.Folder, req.Binding.SecretsBundleName, "")
	keys := c.SSHKeysByFolder[folder]
	if len(keys) == 0 {
		return nil
	}
	out := make([]SSHKeyItem, 0, len(keys))
	for _, key := range keys {
		if key.Name != req.KeyName {
			out = append(out, key)
		}
	}
	c.SSHKeysByFolder[folder] = out
	return nil
}

// NoopClient 未配置 Bitwarden API 时的空实现。
type NoopClient struct{}

func (NoopClient) PullBundle(_ context.Context, _ PullBundleRequest) (*PullBundleResult, error) {
	return &PullBundleResult{}, nil
}

func (NoopClient) PushBundle(_ context.Context, _ PushBundleRequest, _ []SecureNote) (*PushBundleResult, error) {
	return &PushBundleResult{}, nil
}

func (NoopClient) DeleteSecureNote(_ context.Context, _ DeleteSecureNoteRequest) error {
	return nil
}

func (NoopClient) DeleteSSHKey(_ context.Context, _ DeleteSSHKeyRequest) error {
	return nil
}

// DefaultClient 在已配置且有 session + vault key 时返回真实 APIClient，否则 NoopClient。
func DefaultClient() Client {
	if !HasSession() {
		return NoopClient{}
	}
	if !HasUserKey() {
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
