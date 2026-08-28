package secrets

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/types"
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

// ListNotes 枚举某个同步目标下的 Secure Note。
// 远端不存在时返回空列表而非报错，与 PullBundle 一致。
func (c *APIClient) ListNotes(ctx context.Context, target SyncTarget) ([]RemoteNote, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, errVaultKeyNotReady
	}
	sc, err := bwScopeForTarget(target)
	if err != nil {
		return nil, err
	}
	folderID, err := c.findFolderID(ctx, sc.folder, userKey)
	if err != nil || folderID == "" {
		return nil, err
	}
	ciphers, err := c.folderCiphers(ctx, folderID, userKey, sc)
	if err != nil {
		return nil, err
	}

	notes := make([]RemoteNote, 0, len(ciphers))
	for rel, cipher := range ciphers {
		notes = append(notes, RemoteNote{ID: cipher.ID, Name: rel})
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Name < notes[j].Name })
	return notes, nil
}

// GetNote 读取目标下指定 Secure Note 的正文。
// 与 ListNotes 分开，确保候选枚举阶段只暴露元数据；调用方明确选中后才解密正文。
func (c *APIClient) GetNote(ctx context.Context, target SyncTarget, noteRel string) (*SecureNote, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, errVaultKeyNotReady
	}
	sc, err := bwScopeForTarget(target)
	if err != nil {
		return nil, err
	}
	folderID, err := c.findFolderID(ctx, sc.folder, userKey)
	if err != nil {
		return nil, err
	}
	if folderID == "" {
		return nil, fmt.Errorf("远端 %s 不存在", target.Address)
	}
	ciphers, err := c.folderCiphers(ctx, folderID, userKey, sc)
	if err != nil {
		return nil, err
	}
	cipher, ok := ciphers[strings.TrimSpace(noteRel)]
	if !ok {
		return nil, fmt.Errorf("Secure Note %q 不在 %s", noteRel, target.Address)
	}
	itemKey, err := itemDecryptionKey(cipher.Key, userKey)
	if err != nil {
		return nil, err
	}
	content, err := decryptVaultString(cipher.Notes, itemKey)
	if err != nil {
		return nil, fmt.Errorf("解密 Secure Note %q: %w", noteRel, err)
	}
	return &SecureNote{RelativePath: strings.TrimSpace(noteRel), Content: content}, nil
}

// ListSSHKeys 枚举某个同步目标下的 SSH Key（仅元信息）。
func (c *APIClient) ListSSHKeys(ctx context.Context, target SyncTarget) ([]RemoteSSHKey, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, errVaultKeyNotReady
	}
	sc, err := bwScopeForTarget(target)
	if err != nil {
		return nil, err
	}
	folderID, err := c.findFolderID(ctx, sc.folder, userKey)
	if err != nil || folderID == "" {
		return nil, err
	}
	ciphers, err := c.folderSSHKeyCiphers(ctx, folderID, userKey, sc)
	if err != nil {
		return nil, err
	}
	keys := make([]RemoteSSHKey, 0, len(ciphers))
	for rel, cipher := range ciphers {
		keys = append(keys, RemoteSSHKey{ID: cipher.ID, Name: rel})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Name < keys[j].Name })
	return keys, nil
}

// folderCiphers 返回某 folder 下属于 sc 的 Secure Note cipher，按同步根相对路径索引。
// 解密失败的条目静默跳过：vault 里可能混有其他工具写入的、Dec 无法解析的 cipher。
// 前缀不属于 sc 的条目（同一 folder 内的另一平面）同样跳过。
func (c *APIClient) folderCiphers(ctx context.Context, folderID string, userKey []byte, sc bwScope) (map[string]bwCipher, error) {
	return c.scopedCiphers(ctx, folderID, userKey, sc, cipherTypeSecureNote)
}

// folderSSHKeyCiphers 返回某 folder 下属于 sc 的 SSH Key cipher，按逻辑名索引。
func (c *APIClient) folderSSHKeyCiphers(ctx context.Context, folderID string, userKey []byte, sc bwScope) (map[string]bwCipher, error) {
	return c.scopedCiphers(ctx, folderID, userKey, sc, cipherTypeSSHKey)
}

func (c *APIClient) scopedCiphers(ctx context.Context, folderID string, userKey []byte, sc bwScope, cipherType int) (map[string]bwCipher, error) {
	ciphers, err := c.listCiphers(ctx)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bwCipher)
	for _, cipher := range ciphers {
		if cipher.Type != cipherType || cipher.FolderID != folderID {
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
		rel, ok := sc.decode(name)
		if !ok {
			continue
		}
		existing[rel] = cipher
	}
	return existing, nil
}

// folderSSHKeys 解密 folder 内属于 sc 的全部 SSH Key Item。
// 私钥只保存在返回值中，调用方不得写入日志或错误信息。
func (c *APIClient) folderSSHKeys(ctx context.Context, folderID string, userKey []byte, sc bwScope) ([]SSHKeyItem, error) {
	ciphers, err := c.listCiphers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SSHKeyItem, 0)
	for _, cipher := range ciphers {
		if cipher.Type != cipherTypeSSHKey || cipher.FolderID != folderID {
			continue
		}
		item, err := decryptSSHKeyCipher(cipher, userKey, sc)
		if err != nil {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func decryptSSHKeyCipher(cipher bwCipher, userKey []byte, sc bwScope) (SSHKeyItem, error) {
	itemKey, err := itemDecryptionKey(cipher.Key, userKey)
	if err != nil {
		return SSHKeyItem{}, err
	}
	itemName, err := decryptVaultString(strings.TrimSpace(cipher.Name), itemKey)
	if err != nil {
		return SSHKeyItem{}, err
	}
	name, ok := sc.decode(itemName)
	if !ok {
		return SSHKeyItem{}, fmt.Errorf("SSH Key 条目不属于当前寻址域")
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

// ListPNames 枚举远端存在的 P 名（已排序去重）。
func (c *APIClient) ListPNames(ctx context.Context) ([]string, error) {
	all, err := c.ListAddresses(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	names := make([]string, 0)
	for _, address := range all {
		scope, parseErr := ParseRemoteScope(address)
		if parseErr != nil {
			continue
		}
		if _, exists := seen[scope.P]; exists {
			continue
		}
		seen[scope.P] = struct{}{}
		names = append(names, scope.P)
	}
	sort.Strings(names)
	return names, nil
}

// ListAddresses 枚举 vault 中全部可读的远端地址。
//
// P folder 在 Bitwarden 上只有 P 名一级，两个平面靠条目名前缀区分，因此这里按
// 实际存在的前缀展开成逻辑地址 <p>/private/<plane>，让调用方继续只看到「一个
// 地址 = 一个同步单位」。存量非 P folder 原样返回。
func (c *APIClient) ListAddresses(ctx context.Context) ([]string, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, errVaultKeyNotReady
	}
	folders, err := c.listFolders(ctx)
	if err != nil {
		return nil, err
	}
	ciphers, err := c.listCiphers(ctx)
	if err != nil {
		return nil, err
	}

	planesByFolderID := make(map[string]map[SyncPlane]struct{})
	for _, cipher := range ciphers {
		folderID := strings.TrimSpace(cipher.FolderID)
		if folderID == "" {
			continue
		}
		itemName, decErr := decryptCipherName(cipher, userKey)
		if decErr != nil {
			continue
		}
		plane, ok := bwPlaneSegmentOfItemName(itemName)
		if !ok {
			continue
		}
		if planesByFolderID[folderID] == nil {
			planesByFolderID[folderID] = make(map[SyncPlane]struct{})
		}
		planesByFolderID[folderID][plane] = struct{}{}
	}

	seen := make(map[string]struct{})
	names := make([]string, 0)
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for _, folder := range folders {
		rawName, err := decryptVaultString(folder.Name, userKey)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		// 迁移前的旧 folder（名字就是整串逻辑地址）原样返回。
		if scope, parseErr := ParseRemoteScope(name); parseErr == nil {
			add(scope.String())
			continue
		}
		if !types.IsValidPName(name) {
			add(name)
			continue
		}
		planes := planesByFolderID[folder.ID]
		if len(planes) == 0 {
			// 空 P folder：两个平面都列出，Remote 页仍能看见并登记第一条。
			for _, plane := range []SyncPlane{SyncPlaneProject, SyncPlaneMachine} {
				if scope, scopeErr := NewRemoteScope(name, plane); scopeErr == nil {
					add(scope.String())
				}
			}
			continue
		}
		for plane := range planes {
			if scope, scopeErr := NewRemoteScope(name, plane); scopeErr == nil {
				add(scope.String())
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

func (c *StubClient) ListNotes(_ context.Context, target SyncTarget) ([]RemoteNote, error) {
	address := stubAddress(target)
	notes := make([]RemoteNote, 0, len(c.NotesByFolder[address]))
	for _, note := range c.NotesByFolder[address] {
		notes = append(notes, RemoteNote{ID: address + "/" + note.RelativePath, Name: note.RelativePath})
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Name < notes[j].Name })
	return notes, nil
}

func (c *StubClient) ListSSHKeys(_ context.Context, target SyncTarget) ([]RemoteSSHKey, error) {
	address := stubAddress(target)
	keys := make([]RemoteSSHKey, 0, len(c.SSHKeysByFolder[address]))
	for _, key := range c.SSHKeysByFolder[address] {
		id := key.ID
		if id == "" {
			id = address + "/ssh/" + key.Name
		}
		keys = append(keys, RemoteSSHKey{ID: id, Name: key.Name})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Name < keys[j].Name })
	return keys, nil
}

func (c *StubClient) ListPNames(ctx context.Context) ([]string, error) {
	all, err := c.ListAddresses(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for _, address := range all {
		if scope, parseErr := ParseRemoteScope(address); parseErr == nil {
			seen[scope.P] = struct{}{}
		}
	}
	for _, name := range c.SecretBundleFolders {
		if scope, parseErr := ParseRemoteScope(strings.TrimSpace(name)); parseErr == nil {
			seen[scope.P] = struct{}{}
			continue
		}
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			seen[trimmed] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (c *StubClient) ListAddresses(_ context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	for address := range c.NotesByFolder {
		if address = strings.TrimSpace(address); address != "" {
			seen[address] = struct{}{}
		}
	}
	for address := range c.SSHKeysByFolder {
		if address = strings.TrimSpace(address); address != "" {
			seen[address] = struct{}{}
		}
	}
	for _, address := range c.SecretBundleFolders {
		if address = strings.TrimSpace(address); address != "" {
			seen[address] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (NoopClient) ListNotes(_ context.Context, _ SyncTarget) ([]RemoteNote, error) {
	return nil, nil
}

func (NoopClient) ListSSHKeys(_ context.Context, _ SyncTarget) ([]RemoteSSHKey, error) {
	return nil, nil
}

func (NoopClient) ListPNames(_ context.Context) ([]string, error) {
	return nil, nil
}

func (NoopClient) ListAddresses(_ context.Context) ([]string, error) {
	return nil, nil
}

func (c *StubClient) DeleteAddress(_ context.Context, address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return fmt.Errorf("远端地址不能为空")
	}
	if c == nil {
		return nil
	}
	delete(c.NotesByFolder, address)
	delete(c.SSHKeysByFolder, address)
	kept := c.SecretBundleFolders[:0]
	for _, folder := range c.SecretBundleFolders {
		if strings.TrimSpace(folder) == address {
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
