package secrets

import (
	"context"
)

// Client 拉取/推送 Bitwarden secrets bundle 的 API 抽象。
type Client interface {
	PullBundle(ctx context.Context, req PullBundleRequest) (*PullBundleResult, error)
	PushBundle(ctx context.Context, req PushBundleRequest, notes []SecureNote) (*PushBundleResult, error)
	MigrateBundle(ctx context.Context, req MigrateBundleRequest) (*MigrateBundleResult, error)
	DeleteSecureNote(ctx context.Context, req DeleteSecureNoteRequest) error
}

// StubClient 测试/开发用 stub；按 Bitwarden folder 返回预设 Note。
type StubClient struct {
	NotesByFolder map[string][]SecureNote
}

func (c *StubClient) PullBundle(_ context.Context, req PullBundleRequest) (*PullBundleResult, error) {
	folder := req.Binding.SecretsBundleName
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

func (c *StubClient) PushBundle(_ context.Context, req PushBundleRequest, notes []SecureNote) (*PushBundleResult, error) {
	folder := req.Binding.SecretsBundleName
	if folder == "" {
		folder = req.DecBundleName
	}
	if c.NotesByFolder == nil {
		c.NotesByFolder = make(map[string][]SecureNote)
	}
	localNames := make(map[string]struct{}, len(notes))
	for _, note := range notes {
		localNames[note.RelativePath] = struct{}{}
	}
	existing := make(map[string]SecureNote)
	for _, note := range c.NotesByFolder[folder] {
		existing[note.RelativePath] = note
	}
	result := &PushBundleResult{}
	kept := make([]SecureNote, 0, len(notes))
	for _, note := range notes {
		if _, ok := existing[note.RelativePath]; ok {
			result.Updated++
		} else {
			result.Created++
		}
		result.Paths = append(result.Paths, note.RelativePath)
		kept = append(kept, note)
	}
	for path := range existing {
		if _, ok := localNames[path]; !ok {
			result.Deleted++
		}
	}
	c.NotesByFolder[folder] = kept
	return result, nil
}

func (c *StubClient) DeleteSecureNote(_ context.Context, req DeleteSecureNoteRequest) error {
	folder := req.Binding.SecretsBundleName
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

func (c *StubClient) MigrateBundle(_ context.Context, req MigrateBundleRequest) (*MigrateBundleResult, error) {
	folder := req.Binding.SecretsBundleName
	if folder == "" {
		folder = req.DecBundleName
	}
	notes := c.NotesByFolder[folder]
	if len(notes) == 0 {
		return &MigrateBundleResult{}, nil
	}
	result := &MigrateBundleResult{}
	updated := make([]SecureNote, 0, len(notes))
	for _, note := range notes {
		if !NeedsNoteRename(folder, note.RelativePath) {
			updated = append(updated, note)
			continue
		}
		canonical, err := CanonicalNotePath(folder, note.RelativePath)
		if err != nil {
			return nil, err
		}
		if canonical == note.RelativePath {
			updated = append(updated, note)
			continue
		}
		result.RenamedNotes = append(result.RenamedNotes, note.RelativePath+" → "+canonical)
		updated = append(updated, SecureNote{RelativePath: canonical, Content: note.Content})
	}
	c.NotesByFolder[folder] = updated
	return result, nil
}

func notePaths(notes []SecureNote) []string {
	paths := make([]string, 0, len(notes))
	for _, note := range notes {
		paths = append(paths, note.RelativePath)
	}
	return paths
}

// NoopClient 未配置 Bitwarden API 时的空实现。
type NoopClient struct{}

func (NoopClient) PullBundle(_ context.Context, _ PullBundleRequest) (*PullBundleResult, error) {
	return &PullBundleResult{}, nil
}

func (NoopClient) PushBundle(_ context.Context, _ PushBundleRequest, _ []SecureNote) (*PushBundleResult, error) {
	return &PushBundleResult{}, nil
}

func (NoopClient) MigrateBundle(_ context.Context, _ MigrateBundleRequest) (*MigrateBundleResult, error) {
	return &MigrateBundleResult{}, nil
}

func (NoopClient) DeleteSecureNote(_ context.Context, _ DeleteSecureNoteRequest) error {
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
