package secrets

import (
	"context"
	"errors"
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

func (c *StubClient) ListFolderNotes(_ context.Context, folderName string) ([]RemoteNote, error) {
	notes := make([]RemoteNote, 0, len(c.NotesByFolder[folderName]))
	for _, note := range c.NotesByFolder[folderName] {
		notes = append(notes, RemoteNote{ID: folderName + "/" + note.RelativePath, Name: note.RelativePath})
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Name < notes[j].Name })
	return notes, nil
}

func (NoopClient) ListFolderNotes(_ context.Context, _ string) ([]RemoteNote, error) {
	return nil, nil
}
