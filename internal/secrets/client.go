package secrets

import (
	"context"
	"fmt"
	"strings"
)

// Client 拉取/推送 Bitwarden secrets bundle 的 API 抽象。
type Client interface {
	PullBundle(ctx context.Context, req PullBundleRequest) (*PullBundleResult, error)
	PushBundle(ctx context.Context, req PushBundleRequest, notes []SecureNote) (*PushBundleResult, error)
	CreateSSHKey(ctx context.Context, req CreateSSHKeyRequest) error
	DeleteSecureNote(ctx context.Context, req DeleteSecureNoteRequest) error
	DeleteSSHKey(ctx context.Context, req DeleteSSHKeyRequest) error
	UpdateSSHKeyHosts(ctx context.Context, req UpdateSSHKeyHostsRequest) error
	RenameSecureNote(ctx context.Context, req RenameSecureNoteRequest) error
	RenameSSHKey(ctx context.Context, req RenameSSHKeyRequest) error
	// ListNotes 枚举目标下的 note 相对路径（相对 SyncTarget.LocalRoot）。
	ListNotes(ctx context.Context, target SyncTarget) ([]RemoteNote, error)
	// GetNote 读取目标下指定 Secure Note 的正文。
	// 仅在调用方已经通过元数据筛选出目标 Note 后使用，避免为发现候选解密整个 folder。
	GetNote(ctx context.Context, target SyncTarget, noteRel string) (*SecureNote, error)
	// ListSSHKeys 枚举目标下的 SSH Key 逻辑名。
	ListSSHKeys(ctx context.Context, target SyncTarget) ([]RemoteSSHKey, error)
	// ListPNames 枚举远端存在的 P 名。
	ListPNames(ctx context.Context) ([]string, error)
	// ListAddresses 枚举远端全部可读地址：P 展开为 <p>/private/<plane>，存量
	// 非 P folder 用其名字。
	ListAddresses(ctx context.Context) ([]string, error)
	// DeleteAddress 删除存量非 P folder。地址不存在视为成功；仍含条目时由调用方
	// 先清空。P 地址一律拒绝。
	DeleteAddress(ctx context.Context, address string) error
	// ListUnfiledItems 枚举无 folder（FolderID 为空）的条目元数据，不含正文。
	ListUnfiledItems(ctx context.Context) ([]UnfiledItem, error)
}

// UnfiledItem 是 Bitwarden「无文件夹」条目的只读元数据。
type UnfiledItem struct {
	ID   string
	Name string
	Type string // note | ssh | login | card | identity | other
}

// StubClient 测试/开发用 stub；按 Bitwarden folder 返回预设 Note / SSH Key。
type StubClient struct {
	NotesByFolder       map[string][]SecureNote
	SSHKeysByFolder     map[string][]SSHKeyItem
	SecretBundleFolders []string // 逻辑名；用于 ListSecretBundleNames 单测
	UnfiledItems        []UnfiledItem
}

// stubAddress 是 StubClient 的索引键：远端逻辑地址。
func stubAddress(target SyncTarget) string {
	if addr := strings.TrimSpace(target.Address); addr != "" {
		return addr
	}
	return strings.TrimSpace(target.Name)
}

// requireDeleteScope 校验删除目标并解析出远端落点。
// 删除允许只读浏览节点，因此不要求 declared。
func requireDeleteScope(target SyncTarget) (bwScope, error) {
	if stubAddress(target) == "" {
		return bwScope{}, fmt.Errorf("删除目标地址不能为空")
	}
	return bwScopeForTarget(target)
}

func (c *StubClient) PullBundle(_ context.Context, req PullBundleRequest) (*PullBundleResult, error) {
	folder := stubAddress(req.Target)
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

func (c *StubClient) GetNote(_ context.Context, target SyncTarget, noteRel string) (*SecureNote, error) {
	address := stubAddress(target)
	for _, note := range c.NotesByFolder[address] {
		if note.RelativePath == noteRel {
			copied := note
			return &copied, nil
		}
	}
	return nil, fmt.Errorf("Secure Note %q 不在 %s", noteRel, address)
}

func (c *StubClient) PushBundle(_ context.Context, req PushBundleRequest, notes []SecureNote) (*PushBundleResult, error) {
	if err := RequireDeclared(req.Target); err != nil {
		return nil, err
	}
	folder := stubAddress(req.Target)
	if c.NotesByFolder == nil {
		c.NotesByFolder = make(map[string][]SecureNote)
	}
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

func (c *StubClient) CreateSSHKey(_ context.Context, req CreateSSHKeyRequest) error {
	if err := RequireDeclared(req.Target); err != nil {
		return err
	}
	folder := stubAddress(req.Target)
	if c.SSHKeysByFolder == nil {
		c.SSHKeysByFolder = make(map[string][]SSHKeyItem)
	}
	name := strings.TrimSpace(req.Key.Name)
	if _, err := SSHKeyInstance(name); err != nil {
		return err
	}
	for _, key := range c.SSHKeysByFolder[folder] {
		if key.Name == name {
			return fmt.Errorf("SSH Key 已存在: %q", name)
		}
	}
	key := req.Key
	key.Hosts = append([]string(nil), req.Key.Hosts...)
	c.SSHKeysByFolder[folder] = append(c.SSHKeysByFolder[folder], key)
	return nil
}

func (c *StubClient) DeleteSecureNote(_ context.Context, req DeleteSecureNoteRequest) error {
	if _, err := requireDeleteScope(req.Target); err != nil {
		return err
	}
	folder := stubAddress(req.Target)
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
	if _, err := requireDeleteScope(req.Target); err != nil {
		return err
	}
	folder := stubAddress(req.Target)
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

func (c *StubClient) UpdateSSHKeyHosts(_ context.Context, req UpdateSSHKeyHostsRequest) error {
	if err := RequireDeclared(req.Target); err != nil {
		return err
	}
	folder := stubAddress(req.Target)
	keys := c.SSHKeysByFolder[folder]
	for i, key := range keys {
		if key.Name != req.KeyName {
			continue
		}
		hosts := make([]string, 0, len(req.Hosts))
		for _, h := range req.Hosts {
			h = strings.TrimSpace(h)
			if h == "" {
				continue
			}
			hosts = append(hosts, h)
		}
		keys[i].Hosts = hosts
		c.SSHKeysByFolder[folder] = keys
		return nil
	}
	return fmt.Errorf("SSH Key %q 不在 folder %q", req.KeyName, folder)
}

func (c *StubClient) RenameSecureNote(_ context.Context, req RenameSecureNoteRequest) error {
	if err := RequireDeclared(req.Target); err != nil {
		return err
	}
	folder := stubAddress(req.Target)
	oldPath := strings.TrimSpace(req.OldPath)
	newPath := strings.TrimSpace(req.NewPath)
	if oldPath == "" || newPath == "" {
		return fmt.Errorf("RenameSecureNote 需要 OldPath 与 NewPath")
	}
	notes := c.NotesByFolder[folder]
	for i, note := range notes {
		if note.RelativePath != oldPath {
			continue
		}
		for _, other := range notes {
			if other.RelativePath == newPath {
				return fmt.Errorf("目标 Note 已存在: %q", newPath)
			}
		}
		notes[i].RelativePath = newPath
		c.NotesByFolder[folder] = notes
		return nil
	}
	return fmt.Errorf("Secure Note %q 不在 folder %q", oldPath, folder)
}

func (c *StubClient) RenameSSHKey(_ context.Context, req RenameSSHKeyRequest) error {
	if err := RequireDeclared(req.Target); err != nil {
		return err
	}
	folder := stubAddress(req.Target)
	oldName := strings.TrimSpace(req.OldName)
	newName := strings.TrimSpace(req.NewName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("RenameSSHKey 需要 OldName 与 NewName")
	}
	keys := c.SSHKeysByFolder[folder]
	for i, key := range keys {
		if key.Name != oldName {
			continue
		}
		for _, other := range keys {
			if other.Name == newName {
				return fmt.Errorf("目标 SSH Key 已存在: %q", newName)
			}
		}
		keys[i].Name = newName
		c.SSHKeysByFolder[folder] = keys
		return nil
	}
	return fmt.Errorf("SSH Key %q 不在 folder %q", oldName, folder)
}

// NoopClient 未配置 Bitwarden API 时的空实现。
type NoopClient struct{}

func (NoopClient) PullBundle(_ context.Context, _ PullBundleRequest) (*PullBundleResult, error) {
	return &PullBundleResult{}, nil
}

func (NoopClient) GetNote(_ context.Context, target SyncTarget, noteRel string) (*SecureNote, error) {
	return nil, fmt.Errorf("Bitwarden 未解锁，无法读取 %s 下的 %q", target.Address, noteRel)
}

func (NoopClient) PushBundle(_ context.Context, req PushBundleRequest, _ []SecureNote) (*PushBundleResult, error) {
	if err := RequireDeclared(req.Target); err != nil {
		return nil, err
	}
	return &PushBundleResult{}, nil
}

func (NoopClient) CreateSSHKey(_ context.Context, req CreateSSHKeyRequest) error {
	return RequireDeclared(req.Target)
}

func (NoopClient) DeleteSecureNote(_ context.Context, req DeleteSecureNoteRequest) error {
	_, err := requireDeleteScope(req.Target)
	return err
}

func (NoopClient) DeleteSSHKey(_ context.Context, req DeleteSSHKeyRequest) error {
	_, err := requireDeleteScope(req.Target)
	return err
}

func (NoopClient) UpdateSSHKeyHosts(_ context.Context, req UpdateSSHKeyHostsRequest) error {
	return RequireDeclared(req.Target)
}

func (NoopClient) RenameSecureNote(_ context.Context, req RenameSecureNoteRequest) error {
	return RequireDeclared(req.Target)
}

func (NoopClient) RenameSSHKey(_ context.Context, req RenameSSHKeyRequest) error {
	return RequireDeclared(req.Target)
}

func (NoopClient) DeleteAddress(_ context.Context, _ string) error {
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
