package secrets

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var errVaultKeyNotReady = errors.New("Bitwarden vault 密钥未就绪，请重新解锁")

// RemoteNote 是远端一条 Secure Note 的元信息，不含正文。
// Name 是解密后的 note 名，也就是该密文件的项目根相对落地路径。
type RemoteNote struct {
	ID   string
	Name string
}

// RemoteSSHKey 是远端一条 SSH Key 的元信息，不含私钥。
// Name 是逻辑名（用于本地文件名与 Delete 展示）。
type RemoteSSHKey struct {
	ID   string
	Name string
}

// ListFolderNotes 枚举某个 Bitwarden folder 下的 Secure Note。
// folder 不存在时返回空列表而非报错，与 PullBundle 一致。
func (c *APIClient) ListFolderNotes(ctx context.Context, folderName string) ([]RemoteNote, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, errVaultKeyNotReady
	}
	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil || folderID == "" {
		return nil, err
	}
	ciphers, err := c.folderCiphers(ctx, folderID, userKey)
	if err != nil {
		return nil, err
	}

	notes := make([]RemoteNote, 0, len(ciphers))
	for name, cipher := range ciphers {
		notes = append(notes, RemoteNote{ID: cipher.ID, Name: name})
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Name < notes[j].Name })
	return notes, nil
}

// GetSecureNote 读取 folder 下指定 Secure Note 的正文。
// 与 ListFolderNotes 分开，确保候选枚举阶段只暴露元数据；调用方明确选中后才解密正文。
func (c *APIClient) GetSecureNote(ctx context.Context, folderName, noteName string) (*SecureNote, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, errVaultKeyNotReady
	}
	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil {
		return nil, err
	}
	if folderID == "" {
		return nil, fmt.Errorf("Bitwarden folder %q 不存在", folderName)
	}
	ciphers, err := c.folderCiphers(ctx, folderID, userKey)
	if err != nil {
		return nil, err
	}
	cipher, ok := ciphers[strings.TrimSpace(noteName)]
	if !ok {
		return nil, fmt.Errorf("Secure Note %q 不在 folder %q", noteName, folderName)
	}
	itemKey, err := itemDecryptionKey(cipher.Key, userKey)
	if err != nil {
		return nil, err
	}
	content, err := decryptVaultString(cipher.Notes, itemKey)
	if err != nil {
		return nil, fmt.Errorf("解密 Secure Note %q: %w", noteName, err)
	}
	return &SecureNote{RelativePath: strings.TrimSpace(noteName), Content: content}, nil
}

// ListFolderSSHKeys 枚举某个 Bitwarden folder 下的 SSH Key（仅元信息）。
func (c *APIClient) ListFolderSSHKeys(ctx context.Context, folderName string) ([]RemoteSSHKey, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, errVaultKeyNotReady
	}
	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil || folderID == "" {
		return nil, err
	}
	ciphers, err := c.folderSSHKeyCiphers(ctx, folderID, userKey)
	if err != nil {
		return nil, err
	}
	keys := make([]RemoteSSHKey, 0, len(ciphers))
	for name, cipher := range ciphers {
		keys = append(keys, RemoteSSHKey{ID: cipher.ID, Name: name})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Name < keys[j].Name })
	return keys, nil
}

// folderCiphers 返回某 folder 下的 Secure Note cipher，按解密后的 name 索引。
// 解密失败的条目静默跳过：vault 里可能混有其他工具写入的、Dec 无法解析的 cipher。
func (c *APIClient) folderCiphers(ctx context.Context, folderID string, userKey []byte) (map[string]bwCipher, error) {
	ciphers, err := c.listCiphers(ctx)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bwCipher)
	for _, cipher := range ciphers {
		if cipher.Type != cipherTypeSecureNote || cipher.FolderID != folderID {
			continue
		}
		itemKey, err := itemDecryptionKey(cipher.Key, userKey)
		if err != nil {
			continue
		}
		name, err := decryptVaultString(strings.TrimSpace(cipher.Name), itemKey)
		if err != nil || strings.TrimSpace(name) == "" {
			continue
		}
		existing[name] = cipher
	}
	return existing, nil
}

// folderSSHKeyCiphers 返回某 folder 下的 SSH Key cipher，按解密后的逻辑名索引。
func (c *APIClient) folderSSHKeyCiphers(ctx context.Context, folderID string, userKey []byte) (map[string]bwCipher, error) {
	ciphers, err := c.listCiphers(ctx)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bwCipher)
	for _, cipher := range ciphers {
		if cipher.Type != cipherTypeSSHKey || cipher.FolderID != folderID {
			continue
		}
		itemKey, err := itemDecryptionKey(cipher.Key, userKey)
		if err != nil {
			continue
		}
		name, err := decryptVaultString(strings.TrimSpace(cipher.Name), itemKey)
		if err != nil || strings.TrimSpace(name) == "" {
			continue
		}
		existing[name] = cipher
	}
	return existing, nil
}

// folderSSHKeys 解密 folder 内全部 SSH Key Item。
// 私钥只保存在返回值中，调用方不得写入日志或错误信息。
func (c *APIClient) folderSSHKeys(ctx context.Context, folderID string, userKey []byte) ([]SSHKeyItem, error) {
	ciphers, err := c.listCiphers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SSHKeyItem, 0)
	for _, cipher := range ciphers {
		if cipher.Type != cipherTypeSSHKey || cipher.FolderID != folderID {
			continue
		}
		item, err := decryptSSHKeyCipher(cipher, userKey)
		if err != nil {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func decryptSSHKeyCipher(cipher bwCipher, userKey []byte) (SSHKeyItem, error) {
	itemKey, err := itemDecryptionKey(cipher.Key, userKey)
	if err != nil {
		return SSHKeyItem{}, err
	}
	name, err := decryptVaultString(strings.TrimSpace(cipher.Name), itemKey)
	if err != nil {
		return SSHKeyItem{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return SSHKeyItem{}, fmt.Errorf("SSH Key 名称为空")
	}

	hosts := parseSSHHostsNotes("")
	if strings.TrimSpace(cipher.Notes) != "" {
		notes, notesErr := decryptVaultString(cipher.Notes, itemKey)
		if notesErr != nil {
			return SSHKeyItem{}, notesErr
		}
		hosts = parseSSHHostsNotes(notes)
	}

	item := SSHKeyItem{
		ID:    cipher.ID,
		Name:  name,
		Hosts: hosts,
	}
	if cipher.SSHKey == nil {
		return item, fmt.Errorf("SSH Key %q 缺少 sshKey 字段", name)
	}
	if pk := strings.TrimSpace(cipher.SSHKey.PrivateKey); pk != "" {
		plain, err := decryptVaultString(pk, itemKey)
		if err != nil {
			return SSHKeyItem{}, err
		}
		item.PrivateKey = plain
	}
	if pub := strings.TrimSpace(cipher.SSHKey.PublicKey); pub != "" {
		plain, err := decryptVaultString(pub, itemKey)
		if err != nil {
			return SSHKeyItem{}, err
		}
		item.PublicKey = plain
	}
	if fp := strings.TrimSpace(cipher.SSHKey.KeyFingerprint); fp != "" {
		plain, err := decryptVaultString(fp, itemKey)
		if err != nil {
			return SSHKeyItem{}, err
		}
		item.KeyFingerprint = plain
	}
	if strings.TrimSpace(item.PrivateKey) == "" {
		return SSHKeyItem{}, fmt.Errorf("SSH Key %q 缺少私钥", name)
	}
	return item, nil
}

// ListSecretBundleNames 枚举 vault 中所有 bundle/<name> folder，返回逻辑名（已排序去重）。
func (c *APIClient) ListSecretBundleNames(ctx context.Context) ([]string, error) {
	all, err := c.ListAllFolderNames(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	names := make([]string, 0)
	for _, name := range all {
		if pName, _, ok := ParsePFolder(name); ok {
			if _, exists := seen[pName]; !exists {
				seen[pName] = struct{}{}
				names = append(names, pName)
			}
			continue
		}
		if !strings.HasPrefix(name, BundleFolderPrefix) {
			continue
		}
		logical := strings.TrimSpace(strings.TrimPrefix(name, BundleFolderPrefix))
		if logical == "" {
			continue
		}
		if _, ok := seen[logical]; ok {
			continue
		}
		seen[logical] = struct{}{}
		names = append(names, logical)
	}
	sort.Strings(names)
	return names, nil
}

// ListAllFolderNames 枚举 vault 中全部可解密 folder 全名（含 bundle/* 与裸名如 Dec / relkit）。
func (c *APIClient) ListAllFolderNames(ctx context.Context) ([]string, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, errVaultKeyNotReady
	}
	folders, err := c.listFolders(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	names := make([]string, 0)
	for _, folder := range folders {
		rawName, err := decryptVaultString(folder.Name, userKey)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (c *StubClient) ListFolderNotes(_ context.Context, folderName string) ([]RemoteNote, error) {
	notes := make([]RemoteNote, 0, len(c.NotesByFolder[folderName]))
	for _, note := range c.NotesByFolder[folderName] {
		notes = append(notes, RemoteNote{ID: folderName + "/" + note.RelativePath, Name: note.RelativePath})
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Name < notes[j].Name })
	return notes, nil
}

func (c *StubClient) ListFolderSSHKeys(_ context.Context, folderName string) ([]RemoteSSHKey, error) {
	keys := make([]RemoteSSHKey, 0, len(c.SSHKeysByFolder[folderName]))
	for _, key := range c.SSHKeysByFolder[folderName] {
		id := key.ID
		if id == "" {
			id = folderName + "/ssh/" + key.Name
		}
		keys = append(keys, RemoteSSHKey{ID: id, Name: key.Name})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Name < keys[j].Name })
	return keys, nil
}

func (c *StubClient) ListSecretBundleNames(_ context.Context) ([]string, error) {
	all, err := c.ListAllFolderNames(context.Background())
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for _, folder := range all {
		if name, ok := stubSecretBundleName(folder); ok {
			seen[name] = struct{}{}
		}
	}
	for _, folder := range c.SecretBundleFolders {
		name := strings.TrimSpace(folder)
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, BundleFolderPrefix) {
			name = strings.TrimPrefix(name, BundleFolderPrefix)
		}
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (c *StubClient) ListAllFolderNames(_ context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	for folder := range c.NotesByFolder {
		folder = strings.TrimSpace(folder)
		if folder != "" {
			seen[folder] = struct{}{}
		}
	}
	for folder := range c.SSHKeysByFolder {
		folder = strings.TrimSpace(folder)
		if folder != "" {
			seen[folder] = struct{}{}
		}
	}
	for _, folder := range c.SecretBundleFolders {
		folder = strings.TrimSpace(folder)
		if folder == "" {
			continue
		}
		if !strings.HasPrefix(folder, BundleFolderPrefix) {
			// SecretBundleFolders 历史存逻辑名；补全为 bundle/<name> 以便与 NotesByFolder 键一致。
			seen[DefaultBundleFolder(folder)] = struct{}{}
			continue
		}
		seen[folder] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func stubSecretBundleName(folder string) (string, bool) {
	folder = strings.TrimSpace(folder)
	if name, _, ok := ParsePFolder(folder); ok {
		return name, true
	}
	if !strings.HasPrefix(folder, BundleFolderPrefix) {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimPrefix(folder, BundleFolderPrefix))
	return name, name != ""
}

func (NoopClient) ListFolderNotes(_ context.Context, _ string) ([]RemoteNote, error) {
	return nil, nil
}

func (NoopClient) ListFolderSSHKeys(_ context.Context, _ string) ([]RemoteSSHKey, error) {
	return nil, nil
}

func (NoopClient) ListSecretBundleNames(_ context.Context) ([]string, error) {
	return nil, nil
}

func (NoopClient) ListAllFolderNames(_ context.Context) ([]string, error) {
	return nil, nil
}

func (c *StubClient) DeleteFolder(_ context.Context, folderName string) error {
	folderName = strings.TrimSpace(folderName)
	if folderName == "" {
		return fmt.Errorf("folder 名不能为空")
	}
	if c == nil {
		return nil
	}
	delete(c.NotesByFolder, folderName)
	delete(c.SSHKeysByFolder, folderName)
	kept := c.SecretBundleFolders[:0]
	for _, folder := range c.SecretBundleFolders {
		if strings.TrimSpace(folder) == folderName || DefaultBundleFolder(strings.TrimSpace(folder)) == folderName {
			continue
		}
		kept = append(kept, folder)
	}
	c.SecretBundleFolders = kept
	return nil
}

func (NoopClient) ListUnfiledItems(_ context.Context) ([]UnfiledItem, error) {
	return nil, nil
}

func (c *StubClient) ListUnfiledItems(_ context.Context) ([]UnfiledItem, error) {
	if c == nil || len(c.UnfiledItems) == 0 {
		return nil, nil
	}
	out := make([]UnfiledItem, len(c.UnfiledItems))
	copy(out, c.UnfiledItems)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// ListUnfiledItems 枚举 FolderID 为空的 cipher 元数据（不解密正文）。
func (c *APIClient) ListUnfiledItems(ctx context.Context) ([]UnfiledItem, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, errVaultKeyNotReady
	}
	ciphers, err := c.listCiphers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UnfiledItem, 0)
	for _, cipher := range ciphers {
		if strings.TrimSpace(cipher.FolderID) != "" {
			continue
		}
		itemKey, err := itemDecryptionKey(cipher.Key, userKey)
		if err != nil {
			continue
		}
		name, err := decryptVaultString(strings.TrimSpace(cipher.Name), itemKey)
		if err != nil {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, UnfiledItem{
			ID:   cipher.ID,
			Name: name,
			Type: unfiledCipherTypeLabel(cipher.Type),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func unfiledCipherTypeLabel(t int) string {
	switch t {
	case 1:
		return "login"
	case cipherTypeSecureNote:
		return "note"
	case 3:
		return "card"
	case 4:
		return "identity"
	case cipherTypeSSHKey:
		return "ssh"
	default:
		return "other"
	}
}
