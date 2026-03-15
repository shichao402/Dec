package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	indexFile    = "vault.json"
	indexVersion = "v1"
)

// VaultIndex vault 索引，记录所有资产的元数据
type VaultIndex struct {
	Version string      `json:"version"`
	Items   []VaultItem `json:"items"`
}

// VaultItem vault 中的单个资产
type VaultItem struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Path        string   `json:"path"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// LoadIndex 从 vault 目录加载索引
func LoadIndex(vaultDir string) (*VaultIndex, error) {
	indexPath := filepath.Join(vaultDir, indexFile)

	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &VaultIndex{Version: indexVersion}, nil
		}
		return nil, fmt.Errorf("读取索引失败: %w", err)
	}

	var idx VaultIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("解析索引失败: %w", err)
	}

	return &idx, nil
}

// Save 保存索引到 vault 目录
func (idx *VaultIndex) Save(vaultDir string) error {
	idx.Version = indexVersion

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化索引失败: %w", err)
	}

	indexPath := filepath.Join(vaultDir, indexFile)
	if err := os.WriteFile(indexPath, data, 0644); err != nil {
		return fmt.Errorf("写入索引失败: %w", err)
	}

	return nil
}

// AddOrUpdate 添加或更新索引项
// 如果 tags 为 nil，保留已有 tags
func (idx *VaultIndex) AddOrUpdate(item VaultItem) {
	now := time.Now().UTC().Format(time.RFC3339)

	for i, existing := range idx.Items {
		if existing.Type == item.Type && existing.Name == item.Name {
			item.CreatedAt = existing.CreatedAt
			item.UpdatedAt = now
			if item.Tags == nil {
				item.Tags = existing.Tags
			}
			idx.Items[i] = item
			return
		}
	}

	if item.CreatedAt == "" {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	idx.Items = append(idx.Items, item)
}

// Remove 删除索引项，返回是否成功删除
func (idx *VaultIndex) Remove(itemType, name string) bool {
	for i, item := range idx.Items {
		if item.Type == itemType && item.Name == name {
			idx.Items = append(idx.Items[:i], idx.Items[i+1:]...)
			return true
		}
	}
	return false
}

// Find 搜索索引项，匹配 name/description/tags
func (idx *VaultIndex) Find(query string) []VaultItem {
	query = strings.ToLower(query)
	var results []VaultItem

	for _, item := range idx.Items {
		if matchesQuery(item, query) {
			results = append(results, item)
		}
	}

	return results
}

// List 列出指定类型的所有索引项，空字符串返回全部
func (idx *VaultIndex) List(itemType string) []VaultItem {
	if itemType == "" {
		return idx.Items
	}

	var results []VaultItem
	for _, item := range idx.Items {
		if item.Type == itemType {
			results = append(results, item)
		}
	}
	return results
}

// Get 获取指定类型和名称的索引项
func (idx *VaultIndex) Get(itemType, name string) *VaultItem {
	for _, item := range idx.Items {
		if item.Type == itemType && item.Name == name {
			return &item
		}
	}
	return nil
}

func matchesQuery(item VaultItem, query string) bool {
	if strings.Contains(strings.ToLower(item.Name), query) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Description), query) {
		return true
	}
	for _, tag := range item.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}
