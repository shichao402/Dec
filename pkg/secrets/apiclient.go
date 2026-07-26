package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const cipherTypeSecureNote = 2

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

type bwCipher struct {
	ID       string `json:"id"`
	Type     int    `json:"type"`
	Name     string `json:"name"`
	Notes    string `json:"notes"`
	FolderID string `json:"folderId"`
	Key      string `json:"key"`
}

type bwListResponse[T any] struct {
	Data []T `json:"data"`
}

// APIClient 使用 Bitwarden Vault API 拉取 Secure Notes。
type APIClient struct {
	APIURL  string
	Token   string
	HTTP    *http.Client
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

	folderID, err := c.findFolderID(ctx, FolderNameFor(req.Binding, req.DecBundleName), userKey)
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
	return &PullBundleResult{Notes: notes}, nil
}

func (c *APIClient) PushBundle(ctx context.Context, req PushBundleRequest, notes []SecureNote) (*PushBundleResult, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}

	folderName := FolderNameFor(req.Binding, req.DecBundleName)
	folderID, err := c.findFolderID(ctx, folderName, userKey)
	if err != nil {
		return nil, err
	}
	if folderID == "" {
		return nil, fmt.Errorf("Bitwarden folder %q 不存在", folderName)
	}

	existing, err := c.folderCiphers(ctx, folderID, userKey)
	if err != nil {
		return nil, err
	}

	// push 只做 create/update。删除远端 note 必须走 Delete 页的显式单条确认：
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

func (c *APIClient) DeleteSecureNote(ctx context.Context, req DeleteSecureNoteRequest) error {
	userKey := UserKey()
	if len(userKey) == 0 {
		return fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}
	folderName := strings.TrimSpace(req.Binding.SecretsBundleName)
	if folderName == "" {
		return fmt.Errorf("secrets bundle 名称不能为空")
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

func (c *APIClient) deleteCipher(ctx context.Context, cipherID string) error {
	reqURL := strings.TrimRight(c.APIURL, "/") + "/ciphers/" + cipherID
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	applyBitwardenHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", formatAPIError(resp.StatusCode, respBody))
	}
	return nil
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

func (c *APIClient) findFolderID(ctx context.Context, name string, userKey []byte) (string, error) {
	folders, err := c.listFolders(ctx)
	if err != nil {
		return "", err
	}
	for _, folder := range folders {
		decryptedName, err := decryptVaultString(folder.Name, userKey)
		if err != nil {
			return "", fmt.Errorf("解密 Bitwarden folder 名称失败: %w", err)
		}
		if decryptedName == name {
			return folder.ID, nil
		}
	}
	return "", nil
}

func (c *APIClient) listFolders(ctx context.Context) ([]bwFolder, error) {
	var out bwListResponse[bwFolder]
	if err := c.getJSON(ctx, c.APIURL+"/folders", &out); err != nil {
		return nil, fmt.Errorf("列出 Bitwarden folder 失败: %w", err)
	}
	return out.Data, nil
}

func (c *APIClient) listCiphers(ctx context.Context) ([]bwCipher, error) {
	var out bwListResponse[bwCipher]
	if err := c.getJSON(ctx, c.APIURL+"/ciphers", &out); err != nil {
		return nil, fmt.Errorf("列出 Bitwarden cipher 失败: %w", err)
	}
	return out.Data, nil
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
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	applyBitwardenHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", formatAPIError(resp.StatusCode, respBody))
	}
	return nil
}

func (c *APIClient) putJSON(ctx context.Context, reqURL string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	applyBitwardenHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", formatAPIError(resp.StatusCode, respBody))
	}
	return nil
}

func (c *APIClient) getJSON(ctx context.Context, reqURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	applyBitwardenHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", formatAPIError(resp.StatusCode, body))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	return nil
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
