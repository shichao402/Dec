package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const trackingFile = ".dec/tracking.json"

// TrackingData 项目级追踪数据
type TrackingData struct {
	Tracked []TrackedItem `json:"tracked"`
}

// TrackedItem 追踪项
type TrackedItem struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	LocalPath  string   `json:"local_path"`
	LocalPaths []string `json:"local_paths,omitempty"`
	VaultHash  string   `json:"vault_hash"`
	LocalHash  string   `json:"local_hash"`
	LastSync   string   `json:"last_sync"`
}

// ChangeStatus 变更状态
type ChangeStatus struct {
	Item      TrackedItem
	Status    string // "modified", "unchanged", "deleted", "vault_updated"
	LocalHash string
	VaultHash string
}

// LoadTracking 加载项目的追踪数据
func LoadTracking(projectRoot string) (*TrackingData, error) {
	trackPath := filepath.Join(projectRoot, trackingFile)

	data, err := os.ReadFile(trackPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &TrackingData{}, nil
		}
		return nil, fmt.Errorf("读取追踪数据失败: %w", err)
	}

	var td TrackingData
	if err := json.Unmarshal(data, &td); err != nil {
		return nil, fmt.Errorf("解析追踪数据失败: %w", err)
	}

	return &td, nil
}

// Save 保存追踪数据
func (td *TrackingData) Save(projectRoot string) error {
	trackPath := filepath.Join(projectRoot, trackingFile)

	if err := os.MkdirAll(filepath.Dir(trackPath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(trackPath, data, 0644)
}

// Track 添加单路径追踪项
func (td *TrackingData) Track(name, itemType, localPath, hash string) {
	td.TrackPaths(name, itemType, []string{localPath}, hash)
}

// TrackPaths 添加多路径追踪项
func (td *TrackingData) TrackPaths(name, itemType string, localPaths []string, hash string) {
	now := time.Now().UTC().Format(time.RFC3339)
	localPaths = normalizePaths(localPaths)
	primaryPath := ""
	if len(localPaths) > 0 {
		primaryPath = localPaths[0]
	}

	for i, item := range td.Tracked {
		if item.Type == itemType && item.Name == name {
			td.Tracked[i].LocalPath = primaryPath
			td.Tracked[i].LocalPaths = localPaths
			td.Tracked[i].VaultHash = hash
			td.Tracked[i].LocalHash = hash
			td.Tracked[i].LastSync = now
			return
		}
	}

	td.Tracked = append(td.Tracked, TrackedItem{
		Name:       name,
		Type:       itemType,
		LocalPath:  primaryPath,
		LocalPaths: localPaths,
		VaultHash:  hash,
		LocalHash:  hash,
		LastSync:   now,
	})
}

// CheckChanges 检查所有追踪项的变更状态
func (td *TrackingData) CheckChanges(projectRoot string, v *Vault) []ChangeStatus {
	var changes []ChangeStatus

	for _, item := range td.Tracked {
		localPaths := item.LocalPaths
		if len(localPaths) == 0 && item.LocalPath != "" {
			localPaths = []string{item.LocalPath}
		}
		if len(localPaths) == 0 {
			localPaths = []string{""}
		}

		for _, localPath := range localPaths {
			itemForPath := item
			itemForPath.LocalPath = localPath

			cs := ChangeStatus{Item: itemForPath, Status: "unchanged"}

			localAbs := filepath.Join(projectRoot, localPath)
			localHash, err := hashPath(localAbs)
			cs.LocalHash = localHash

			if err != nil {
				cs.Status = "deleted"
				changes = append(changes, cs)
				continue
			}

			if localHash != item.LocalHash && localHash != item.VaultHash {
				cs.Status = "modified"
			}

			if v != nil {
				vaultItem := v.Index.Get(item.Type, item.Name)
				if vaultItem != nil {
					vaultPath := filepath.Join(v.Dir, vaultItem.Path)
					vaultHash, _ := hashPath(vaultPath)
					cs.VaultHash = vaultHash

					if vaultHash != item.VaultHash && cs.Status != "modified" {
						cs.Status = "vault_updated"
					}
				}
			}

			if cs.Status != "unchanged" {
				changes = append(changes, cs)
			}
		}
	}

	return changes
}

func normalizePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var result []string
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

// HashPath 计算文件或目录的 SHA256 哈希
func HashPath(path string) (string, error) {
	return hashPath(path)
}

func hashPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	if !info.IsDir() {
		return hashFile(path)
	}

	return hashDir(path)
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashDir(dir string) (string, error) {
	h := sha256.New()

	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(files)

	for _, f := range files {
		h.Write([]byte(f))
		fh, err := hashFile(filepath.Join(dir, f))
		if err != nil {
			return "", err
		}
		h.Write([]byte(fh))
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// FormatChanges 格式化变更列表为可读字符串
func FormatChanges(changes []ChangeStatus) string {
	if len(changes) == 0 {
		return "所有追踪项均无变化"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("检测到 %d 个变更:\n", len(changes)))

	for _, cs := range changes {
		var icon string
		switch cs.Status {
		case "modified":
			icon = "M"
		case "deleted":
			icon = "D"
		case "vault_updated":
			icon = "U"
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s/%s (%s)\n", icon, cs.Item.Type, cs.Item.Name, cs.Item.LocalPath))
	}

	sb.WriteString("\n标记说明: [M]=本地已修改  [D]=本地已删除  [U]=Vault 有更新")
	return sb.String()
}
