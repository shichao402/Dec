package secrets

import (
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

type bwCipher struct {
	Type     int    `json:"type"`
	Name     string `json:"name"`
	Notes    string `json:"notes"`
	FolderID string `json:"folderId"`
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
	folderName := req.Binding.BitwardenFolder
	if folderName == "" {
		folderName = req.Binding.SecretsBundleName
	}
	if folderName == "" {
		folderName = req.DecBundleName
	}

	folderID, err := c.findFolderID(ctx, folderName)
	if err != nil {
		return nil, err
	}
	if folderID == "" {
		return &PullBundleResult{}, nil
	}

	ciphers, err := c.listCiphers(ctx)
	if err != nil {
		return nil, err
	}

	notes := make([]SecureNote, 0)
	for _, cipher := range ciphers {
		if cipher.Type != cipherTypeSecureNote {
			continue
		}
		if cipher.FolderID != folderID {
			continue
		}
		name := strings.TrimSpace(cipher.Name)
		if name == "" {
			continue
		}
		notes = append(notes, SecureNote{
			RelativePath: name,
			Content:      cipher.Notes,
		})
	}
	return &PullBundleResult{Notes: notes}, nil
}

func (c *APIClient) findFolderID(ctx context.Context, name string) (string, error) {
	folders, err := c.listFolders(ctx)
	if err != nil {
		return "", err
	}
	for _, folder := range folders {
		if folder.Name == name {
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

func (c *APIClient) getJSON(ctx context.Context, reqURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
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
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, trimBody(body))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	return nil
}

var _ Client = (*APIClient)(nil)
