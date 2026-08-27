package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

const (
	cipherTypeSecureNote = 2
	cipherTypeSSHKey     = 5
)

type bwFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type bwErrorResponse struct {
	Message          string              `json:"message"`
	ValidationErrors map[string][]string `json:"validationErrors"`
	Object           string              `json:"object"`
	Error            string              `json:"error"`
	ErrorDescription string              `json:"error_description"`
}

type bwSSHKey struct {
	PrivateKey     string `json:"privateKey"`
	PublicKey      string `json:"publicKey"`
	KeyFingerprint string `json:"keyFingerprint"`
}

type bwCipher struct {
	ID       string    `json:"id"`
	Type     int       `json:"type"`
	Name     string    `json:"name"`
	Notes    string    `json:"notes"`
	FolderID string    `json:"folderId"`
	Key      string    `json:"key"`
	SSHKey   *bwSSHKey `json:"sshKey"`
}

type bwListResponse[T any] struct {
	Data []T `json:"data"`
}

// APIClient 使用 Bitwarden Vault API 拉取 Secure Notes。
//
// 一次浏览可能要跨十几个 folder 取 Note / SSH Key，而 Bitwarden 只提供整库
// /ciphers 与 /folders 列表接口。实例内缓存这两份快照，让「列 N 个 folder」
// 退化为一次下载；任何写操作后立即失效。DefaultClient 每次调用都新建实例，
// 缓存生命周期天然就是一次业务操作。
type APIClient struct {
	APIURL string
	Token  string
	HTTP   *http.Client

	snapshotMu sync.Mutex
	folders    []bwFolder
	foldersOK  bool
	ciphers    []bwCipher
	ciphersOK  bool
}

// NewAPIClient 创建带 session 的 Vault API 客户端。
func NewAPIClient(cfg *Config, token string, httpClient *http.Client) (*APIClient, error) {
	if cfg == nil || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("Bitwarden API 客户端缺少 session")
	}
	_, apiURL, err := cfg.Endpoints()
	if err != nil {
		return nil, err
	}
	return &APIClient{
		APIURL: apiURL,
		Token:  token,
		HTTP:   httpClient,
	}, nil
}

func (c *APIClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *APIClient) PullBundle(ctx context.Context, req PullBundleRequest) (*PullBundleResult, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}

	folderID, err := c.findFolderID(ctx, folderNameForRequest(req.Target.Folder, req.Binding, req.DecBundleName), userKey)
	if err != nil {
		return nil, err
	}
	if folderID == "" {
		return &PullBundleResult{}, nil
	}

	existing, err := c.folderCiphers(ctx, folderID, userKey)
	if err != nil {
		return nil, err
	}

	notes := make([]SecureNote, 0, len(existing))
	for name, cipher := range existing {
		itemKey, err := itemDecryptionKey(cipher.Key, userKey)
		if err != nil {
			continue
		}
		content, err := decryptVaultString(cipher.Notes, itemKey)
		if err != nil {
			continue
		}
		notes = append(notes, SecureNote{RelativePath: name, Content: content})
	}

	sshKeys, err := c.folderSSHKeys(ctx, folderID, userKey)
	if err != nil {
		return nil, err
	}
	return &PullBundleResult{Notes: notes, SSHKeys: sshKeys}, nil
}

func (c *APIClient) PushBundle(ctx context.Context, req PushBundleRequest, notes []SecureNote) (*PushBundleResult, error) {
	if err := RequireDeclared(req.Target); err != nil {
		return nil, err
	}
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}

	folderName := folderNameForRequest(req.Target.Folder, req.Binding, req.DecBundleName)
	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil {
		return nil, err
	}
	if folderID == "" {
		if !req.CreateFolderIfMissing {
			return nil, fmt.Errorf("Bitwarden folder %q 不存在", folderName)
		}
		folderID, err = c.createFolder(ctx, folderName, userKey)
		if err != nil {
			return nil, fmt.Errorf("创建 Bitwarden folder %q 失败: %w", folderName, err)
		}
	}

	existing, err := c.folderCiphers(ctx, folderID, userKey)
	if err != nil {
		return nil, err
	}

	// push 只做 create/update。删除远端 note 必须走 Remote 页的显式单条确认：
	// 落地路径散在项目根，不存在可枚举的权威本地集合，靠"本地没有就删远端"
	// 会在枚举漏一个文件时静默删掉一条真密钥。
	result := &PushBundleResult{}
	for _, note := range notes {
		noteName := strings.TrimSpace(note.RelativePath)
		if noteName == "" {
			continue
		}
		cipher, ok := findExistingCipher(existing, noteName)
		if ok {
			if err := c.updateSecureNote(ctx, cipher, userKey, note.Content); err != nil {
				return nil, fmt.Errorf("更新 Secure Note %q 失败: %w", noteName, err)
			}
			result.Updated++
		} else {
			if err := c.createSecureNote(ctx, folderID, userKey, noteName, note.Content); err != nil {
				return nil, fmt.Errorf("创建 Secure Note %q 失败: %w", noteName, err)
			}
			result.Created++
		}
		result.Paths = append(result.Paths, noteName)
	}
	return result, nil
}

func (c *APIClient) CreateSSHKey(ctx context.Context, req CreateSSHKeyRequest) error {
	if err := RequireDeclared(req.Target); err != nil {
		return err
	}
	userKey := UserKey()
	if len(userKey) == 0 {
		return fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}
	folderName := folderNameForRequest(req.Target.Folder, req.Binding, "")
	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil {
		return err
	}
	if folderID == "" {
		if !req.CreateFolderIfMissing {
			return fmt.Errorf("Bitwarden folder %q 不存在", folderName)
		}
		folderID, err = c.createFolder(ctx, folderName, userKey)
		if err != nil {
			return fmt.Errorf("创建 Bitwarden folder %q 失败: %w", folderName, err)
		}
	}

	key := req.Key
	key.Name = strings.TrimSpace(key.Name)
	if _, err := SSHKeyInstance(key.Name); err != nil {
		return err
	}
	if err := validateSSHKeyMaterial(key.PrivateKey); err != nil {
		return fmt.Errorf("SSH Key %q 私钥格式无效", key.Name)
	}
	if strings.TrimSpace(key.PublicKey) == "" {
		return fmt.Errorf("SSH Key %q 缺少公钥", key.Name)
	}
	if strings.TrimSpace(key.KeyFingerprint) == "" {
		return fmt.Errorf("SSH Key %q 缺少 fingerprint", key.Name)
	}
	key.Hosts, err = NormalizeSSHHosts(key.Hosts)
	if err != nil {
		return err
	}

	existing, err := c.folderSSHKeyCiphers(ctx, folderID, userKey)
	if err != nil {
		return err
	}
	if _, exists := existing[key.Name]; exists {
		return fmt.Errorf("SSH Key 已存在: %q", key.Name)
	}
	return c.createSSHKey(ctx, folderID, userKey, key)
}

func (c *APIClient) DeleteSecureNote(ctx context.Context, req DeleteSecureNoteRequest) error {
	folderName, err := requireDeleteFolder(req.Target, req.Binding)
	if err != nil {
		return err
	}
	userKey := UserKey()
	if len(userKey) == 0 {
		return fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}
	notePath := strings.TrimSpace(req.NotePath)
	if notePath == "" {
		return fmt.Errorf("Secure Note 路径不能为空")
	}

	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil {
		return err
	}
	if folderID == "" {
		return nil
	}

	existing, err := c.folderCiphers(ctx, folderID, userKey)
	if err != nil {
		return err
	}

	cipher, ok := findExistingCipher(existing, notePath)
	if !ok {
		return nil
	}
	return c.deleteCipher(ctx, cipher.ID)
}

func (c *APIClient) DeleteFolder(ctx context.Context, folderName string) error {
	folderName = strings.TrimSpace(folderName)
	if folderName == "" {
		return fmt.Errorf("folder 名不能为空")
	}
	if _, _, ok := ParsePFolder(folderName); ok {
		return fmt.Errorf("拒绝删除 P folder %q", folderName)
	}
	userKey := UserKey()
	if len(userKey) == 0 {
		return fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}
	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil {
		return err
	}
	if folderID == "" {
		return nil
	}
	c.invalidateSnapshot()
	reqURL := strings.TrimRight(c.APIURL, "/") + "/folders/" + folderID
	return c.doAuthenticatedJSON(ctx, http.MethodDelete, reqURL, nil, nil)
}

func (c *APIClient) createSSHKey(ctx context.Context, folderID string, userKey []byte, key SSHKeyItem) error {
	itemKey, err := generateCipherKey()
	if err != nil {
		return err
	}
	encrypt := func(label, plain string) (string, error) {
		enc, encErr := encryptVaultString(strings.TrimSpace(plain), itemKey)
		if encErr != nil {
			return "", fmt.Errorf("加密 SSH Key %s 失败: %w", label, encErr)
		}
		return enc, nil
	}
	encName, err := encrypt("名称", key.Name)
	if err != nil {
		return err
	}
	encPrivate, err := encrypt("私钥", key.PrivateKey)
	if err != nil {
		return err
	}
	encPublic, err := encrypt("公钥", key.PublicKey)
	if err != nil {
		return err
	}
	encFingerprint, err := encrypt("fingerprint", key.KeyFingerprint)
	if err != nil {
		return err
	}
	encNotes, err := encryptNoteField(formatSSHHostsNotes(key.Hosts), itemKey)
	if err != nil {
		return err
	}
	encItemKey, err := encryptVaultBytes(itemKey, userKey)
	if err != nil {
		return err
	}
	return c.postJSON(ctx, c.APIURL+"/ciphers", map[string]any{
		"type":     cipherTypeSSHKey,
		"name":     encName,
		"notes":    encNotes,
		"folderId": folderID,
		"favorite": false,
		"key":      encItemKey,
		"sshKey": map[string]any{
			"privateKey":     encPrivate,
			"publicKey":      encPublic,
			"keyFingerprint": encFingerprint,
		},
	})
}

func (c *APIClient) DeleteSSHKey(ctx context.Context, req DeleteSSHKeyRequest) error {
	folderName, err := requireDeleteFolder(req.Target, req.Binding)
	if err != nil {
		return err
	}
	userKey := UserKey()
	if len(userKey) == 0 {
		return fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}
	keyName := strings.TrimSpace(req.KeyName)
	if keyName == "" {
		return fmt.Errorf("SSH Key 名称不能为空")
	}

	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil {
		return err
	}
	if folderID == "" {
		return nil
	}

	existing, err := c.folderSSHKeyCiphers(ctx, folderID, userKey)
	if err != nil {
		return err
	}
	cipher, ok := existing[keyName]
	if !ok {
		return nil
	}
	return c.deleteCipher(ctx, cipher.ID)
}

func (c *APIClient) UpdateSSHKeyHosts(ctx context.Context, req UpdateSSHKeyHostsRequest) error {
	if err := RequireDeclared(req.Target); err != nil {
		return err
	}
	userKey := UserKey()
	if len(userKey) == 0 {
		return fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}
	folderName := strings.TrimSpace(req.Binding.SecretsBundleName)
	if folderName == "" {
		folderName = strings.TrimSpace(req.Target.Folder)
	}
	if folderName == "" {
		return fmt.Errorf("secrets bundle 名称不能为空")
	}
	keyName := strings.TrimSpace(req.KeyName)
	if keyName == "" {
		return fmt.Errorf("SSH Key 名称不能为空")
	}

	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil {
		return err
	}
	if folderID == "" {
		return fmt.Errorf("Bitwarden folder %q 不存在", folderName)
	}

	existing, err := c.folderSSHKeyCiphers(ctx, folderID, userKey)
	if err != nil {
		return err
	}
	cipher, ok := existing[keyName]
	if !ok {
		return fmt.Errorf("SSH Key %q 不在 folder %q", keyName, folderName)
	}
	return c.updateSSHKeyNotes(ctx, cipher, userKey, formatSSHHostsNotes(req.Hosts))
}

func (c *APIClient) RenameSecureNote(ctx context.Context, req RenameSecureNoteRequest) error {
	if err := RequireDeclared(req.Target); err != nil {
		return err
	}
	userKey := UserKey()
	if len(userKey) == 0 {
		return fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}
	folderName := strings.TrimSpace(req.Binding.SecretsBundleName)
	if folderName == "" {
		folderName = strings.TrimSpace(req.Target.Folder)
	}
	if folderName == "" {
		return fmt.Errorf("secrets bundle 名称不能为空")
	}
	oldPath := strings.TrimSpace(req.OldPath)
	newPath := strings.TrimSpace(req.NewPath)
	if oldPath == "" || newPath == "" {
		return fmt.Errorf("RenameSecureNote 需要 OldPath 与 NewPath")
	}
	if _, err := normalizeSyncRelPath(newPath); err != nil {
		return err
	}

	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil {
		return err
	}
	if folderID == "" {
		return fmt.Errorf("Bitwarden folder %q 不存在", folderName)
	}
	existing, err := c.folderCiphers(ctx, folderID, userKey)
	if err != nil {
		return err
	}
	cipher, ok := findExistingCipher(existing, oldPath)
	if !ok {
		return fmt.Errorf("Secure Note %q 不在 folder %q", oldPath, folderName)
	}
	if _, conflict := findExistingCipher(existing, newPath); conflict {
		return fmt.Errorf("目标 Note 已存在: %q", newPath)
	}
	return c.renameCipherName(ctx, cipher, userKey, newPath, cipherTypeSecureNote)
}

func (c *APIClient) RenameSSHKey(ctx context.Context, req RenameSSHKeyRequest) error {
	if err := RequireDeclared(req.Target); err != nil {
		return err
	}
	userKey := UserKey()
	if len(userKey) == 0 {
		return fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}
	folderName := strings.TrimSpace(req.Binding.SecretsBundleName)
	if folderName == "" {
		folderName = strings.TrimSpace(req.Target.Folder)
	}
	if folderName == "" {
		return fmt.Errorf("secrets bundle 名称不能为空")
	}
	oldName := strings.TrimSpace(req.OldName)
	newName := strings.TrimSpace(req.NewName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("RenameSSHKey 需要 OldName 与 NewName")
	}

	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil {
		return err
	}
	if folderID == "" {
		return fmt.Errorf("Bitwarden folder %q 不存在", folderName)
	}
	existing, err := c.folderSSHKeyCiphers(ctx, folderID, userKey)
	if err != nil {
		return err
	}
	cipher, ok := existing[oldName]
	if !ok {
		return fmt.Errorf("SSH Key %q 不在 folder %q", oldName, folderName)
	}
	if _, conflict := existing[newName]; conflict {
		return fmt.Errorf("目标 SSH Key 已存在: %q", newName)
	}
	return c.renameCipherName(ctx, cipher, userKey, newName, cipherTypeSSHKey)
}

// renameCipherName 用新明文名重新加密 name 字段，其余字段原样回传。
func (c *APIClient) renameCipherName(ctx context.Context, cipher bwCipher, userKey []byte, newName string, cipherType int) error {
	itemKey, err := itemDecryptionKey(cipher.Key, userKey)
	if err != nil {
		return err
	}
	encName, err := encryptVaultString(newName, itemKey)
	if err != nil {
		return err
	}
	body := map[string]any{
		"type":     cipherType,
		"name":     encName,
		"notes":    optionalCipherField(cipher.Notes),
		"folderId": cipher.FolderID,
		"favorite": false,
	}
	if cipherType == cipherTypeSecureNote {
		body["secureNote"] = secureNotePayload()
	}
	if cipherType == cipherTypeSSHKey {
		body["sshKey"] = cipher.SSHKey
	}
	if strings.TrimSpace(cipher.Key) != "" {
		body["key"] = cipher.Key
	}
	return c.putJSON(ctx, c.APIURL+"/ciphers/"+cipher.ID, body)
}

func (c *APIClient) updateSSHKeyNotes(ctx context.Context, cipher bwCipher, userKey []byte, notesPlain string) error {
	itemKey, err := itemDecryptionKey(cipher.Key, userKey)
	if err != nil {
		return err
	}
	encNotes, err := encryptNoteField(notesPlain, itemKey)
	if err != nil {
		return err
	}
	body := map[string]any{
		"type":     cipherTypeSSHKey,
		"name":     cipher.Name,
		"notes":    encNotes,
		"folderId": cipher.FolderID,
		"favorite": false,
		"sshKey":   cipher.SSHKey,
	}
	if strings.TrimSpace(cipher.Key) != "" {
		body["key"] = cipher.Key
	}
	return c.putJSON(ctx, c.APIURL+"/ciphers/"+cipher.ID, body)
}

func (c *APIClient) deleteCipher(ctx context.Context, cipherID string) error {
	c.invalidateSnapshot()
	reqURL := strings.TrimRight(c.APIURL, "/") + "/ciphers/" + cipherID
	return c.doAuthenticatedJSON(ctx, http.MethodDelete, reqURL, nil, nil)
}

// findExistingCipher 按 note 名精确匹配。
// note 名就是落地路径，只有一种合法形态，不存在需要多形态互认的命名变体。
func findExistingCipher(existing map[string]bwCipher, noteName string) (bwCipher, bool) {
	cipher, ok := existing[strings.TrimSpace(noteName)]
	return cipher, ok
}

func secureNotePayload() map[string]any {
	return map[string]any{"type": 0}
}

// optionalCipherField 把空的密文字段回传成 null。Bitwarden 按 EncryptedString
// 校验这些字段，空字符串会被判为非法密文（400 model state is invalid）。
func optionalCipherField(enc string) any {
	if strings.TrimSpace(enc) == "" {
		return nil
	}
	return enc
}

func encryptNoteField(plain string, itemKey []byte) (any, error) {
	if plain == "" {
		return nil, nil
	}
	enc, err := encryptVaultString(plain, itemKey)
	if err != nil {
		return nil, err
	}
	return enc, nil
}

func (c *APIClient) createSecureNote(ctx context.Context, folderID string, userKey []byte, name, content string) error {
	itemKey, err := generateCipherKey()
	if err != nil {
		return err
	}
	encName, err := encryptVaultString(name, itemKey)
	if err != nil {
		return err
	}
	encNotes, err := encryptNoteField(content, itemKey)
	if err != nil {
		return err
	}
	encItemKey, err := encryptVaultBytes(itemKey, userKey)
	if err != nil {
		return err
	}
	body := map[string]any{
		"type":       cipherTypeSecureNote,
		"name":       encName,
		"notes":      encNotes,
		"folderId":   folderID,
		"favorite":   false,
		"key":        encItemKey,
		"secureNote": secureNotePayload(),
	}
	return c.postJSON(ctx, c.APIURL+"/ciphers", body)
}

// updateSecureNote 只更新正文，**原样回传远端的 name 密文**。
// push 是「按远端 note 列表同步正文」，无权改名：note 名就是落地路径，
// 改名等于把这条 secret 指到另一个文件上，只能由用户在 Bitwarden 里显式改。
func (c *APIClient) updateSecureNote(ctx context.Context, cipher bwCipher, userKey []byte, content string) error {
	itemKey, err := itemDecryptionKey(cipher.Key, userKey)
	if err != nil {
		return err
	}
	encNotes, err := encryptNoteField(content, itemKey)
	if err != nil {
		return err
	}
	body := map[string]any{
		"type":       cipherTypeSecureNote,
		"name":       cipher.Name,
		"notes":      encNotes,
		"folderId":   cipher.FolderID,
		"favorite":   false,
		"secureNote": secureNotePayload(),
	}
	// Bitwarden PUT 要求回传原有 cipher key；省略会导致服务端清空 key，
	// 而 name/notes 仍用 item key 加密，后续 pull 会 MAC 校验失败。
	if strings.TrimSpace(cipher.Key) != "" {
		body["key"] = cipher.Key
	}
	return c.putJSON(ctx, c.APIURL+"/ciphers/"+cipher.ID, body)
}

// createFolder 在 Bitwarden 新建 folder（name 用 user key 直接加密，无 item key），
// 建完使快照失效并回查 ID。仅供 Remote 登记新 folder 使用。
func (c *APIClient) createFolder(ctx context.Context, name string, userKey []byte) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("folder 名不能为空")
	}
	encName, err := encryptVaultString(name, userKey)
	if err != nil {
		return "", err
	}
	if err := c.postJSON(ctx, c.APIURL+"/folders", map[string]any{"name": encName}); err != nil {
		return "", err
	}
	// postJSON 已使快照失效，findFolderID 会重新拉取并命中新 folder。
	id, err := c.findFolderID(ctx, name, userKey)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("folder 已创建但未能回查到 ID")
	}
	return id, nil
}

func (c *APIClient) findFolderID(ctx context.Context, name string, userKey []byte) (string, error) {
	folders, err := c.listFolders(ctx)
	if err != nil {
		return "", err
	}
	for _, folder := range folders {
		// 解不开的 folder 属于 Dec 读不懂的条目（例如 organization 共享），
		// 跳过而不是让整次查找失败——否则一个外来 folder 会拖垮全部浏览。
		decryptedName, err := decryptVaultString(folder.Name, userKey)
		if err != nil {
			continue
		}
		if decryptedName == name {
			return folder.ID, nil
		}
	}
	return "", nil
}

func (c *APIClient) listFolders(ctx context.Context) ([]bwFolder, error) {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	if c.foldersOK {
		return c.folders, nil
	}
	var out bwListResponse[bwFolder]
	if err := c.getJSON(ctx, c.APIURL+"/folders", &out); err != nil {
		return nil, fmt.Errorf("列出 Bitwarden folder 失败: %w", err)
	}
	c.folders = out.Data
	c.foldersOK = true
	return c.folders, nil
}

func (c *APIClient) listCiphers(ctx context.Context) ([]bwCipher, error) {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	if c.ciphersOK {
		return c.ciphers, nil
	}
	var out bwListResponse[bwCipher]
	if err := c.getJSON(ctx, c.APIURL+"/ciphers", &out); err != nil {
		return nil, fmt.Errorf("列出 Bitwarden cipher 失败: %w", err)
	}
	c.ciphers = out.Data
	c.ciphersOK = true
	return c.ciphers, nil
}

// invalidateSnapshot 丢弃 folder / cipher 快照，任何写操作后必须调用。
func (c *APIClient) invalidateSnapshot() {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	c.folders = nil
	c.foldersOK = false
	c.ciphers = nil
	c.ciphersOK = false
}

func itemDecryptionKey(encryptedKey string, userKey []byte) ([]byte, error) {
	if strings.TrimSpace(encryptedKey) == "" {
		return userKey, nil
	}
	itemKey, err := decryptVaultBytes(encryptedKey, userKey)
	if err != nil {
		return nil, err
	}
	if len(itemKey) != 64 {
		return nil, fmt.Errorf("cipher key 长度异常: %d", len(itemKey))
	}
	return itemKey, nil
}

func (c *APIClient) postJSON(ctx context.Context, reqURL string, body any) error {
	c.invalidateSnapshot()
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.doAuthenticatedJSON(ctx, http.MethodPost, reqURL, data, nil)
}

func (c *APIClient) putJSON(ctx context.Context, reqURL string, body any) error {
	c.invalidateSnapshot()
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.doAuthenticatedJSON(ctx, http.MethodPut, reqURL, data, nil)
}

func (c *APIClient) getJSON(ctx context.Context, reqURL string, dest any) error {
	return c.doAuthenticatedJSON(ctx, http.MethodGet, reqURL, nil, dest)
}

var reauthenticateSession = func(ctx context.Context) error {
	return EnsureSession(ctx, nil)
}

// doAuthenticatedJSON 在 Bitwarden 拒绝过期 session 时清除内存凭据、重新解锁，
// 并仅重试当前请求一次。请求体由字节切片重建，可安全重放 POST / PUT。
func (c *APIClient) doAuthenticatedJSON(
	ctx context.Context,
	method, reqURL string,
	requestBody []byte,
	dest any,
) error {
	for attempt := 0; attempt < 2; attempt++ {
		token := c.Token
		var bodyReader io.Reader
		if requestBody != nil {
			bodyReader = bytes.NewReader(requestBody)
		}
		req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if requestBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		applyBitwardenHeaders(req)

		resp, err := c.httpClient().Do(req)
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode == http.StatusUnauthorized {
			InvalidateSession(token)
			if attempt == 0 {
				if err := reauthenticateSession(ctx); err != nil {
					return fmt.Errorf("Bitwarden session 已失效，重新解锁失败: %w", err)
				}
				c.Token = Session()
				if strings.TrimSpace(c.Token) == "" {
					return fmt.Errorf("Bitwarden session 已失效，重新解锁未返回 session")
				}
				continue
			}
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s", formatAPIError(resp.StatusCode, body))
		}
		if dest != nil {
			if err := json.Unmarshal(body, dest); err != nil {
				return fmt.Errorf("解析响应失败: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("Bitwarden session 重新解锁后仍不可用")
}

func formatAPIError(status int, body []byte) string {
	var errResp bwErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		msg := strings.TrimSpace(errResp.Message)
		if msg == "" {
			msg = strings.TrimSpace(errResp.ErrorDescription)
		}
		if msg == "" {
			msg = strings.TrimSpace(errResp.Error)
		}
		if msg != "" {
			if len(errResp.ValidationErrors) > 0 {
				var parts []string
				for field, errs := range errResp.ValidationErrors {
					parts = append(parts, fmt.Sprintf("%s: %s", field, strings.Join(errs, "; ")))
				}
				msg = msg + " (" + strings.Join(parts, "; ") + ")"
			}
			return fmt.Sprintf("HTTP %d: %s", status, msg)
		}
	}
	return fmt.Sprintf("HTTP %d: %s", status, trimBody(body))
}

var _ Client = (*APIClient)(nil)
