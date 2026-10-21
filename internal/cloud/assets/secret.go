package assets

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 密钥存储（需求 8）。
//
// 每条密钥对应仓库内 secrets/<name>.json，内容是：
//
//	{ "v": 1, "name": ..., "notes": ..., "refs": [...], "updated_at": ...,
//	  "sealed": { 密文 } }
//
// 明文永不落盘。refs 是引用关系的权威记录，由写入方显式声明；
// 渲染时如果发现资产引用了未登记的 refs，会按内容再算一次（见 RefsOf）。

// secretFile 是 secrets/<name>.json 的结构。
type secretFile struct {
	V         int               `json:"v"`
	Name      string            `json:"name"`
	Notes     string            `json:"notes,omitempty"`
	Refs      []proto.SecretRef `json:"refs,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
	Sealed    *crypt.Sealed     `json:"sealed"`
	// Devices 是曾经下发过这条密钥的设备 ID 集合，用于吊销后的轮换清单（需求 18.11）。
	Devices []string `json:"devices,omitempty"`
}

const secretFileVersion = 1

// PutSecretInput 是一次密钥写入。
type PutSecretInput struct {
	Name         string
	Value        string
	Notes        string
	Refs         []proto.SecretRef
	BaseRevision string
	Message      string
	Author       ssot.Author
}

// PutSecret 加密并写入一条密钥。
//
// 明文只在这次调用里存在，写入完成后立刻归还给调用方清零。
func (w *Writer) PutSecret(key *crypt.Key, in PutSecretInput) (string, error) {
	if key == nil {
		return "", errors.New("根密钥未加载")
	}
	if err := ssot.ValidateItemName(in.Name); err != nil {
		return "", err
	}
	if in.Value == "" {
		return "", fmt.Errorf("%w: 密钥值不能为空", ErrSchema)
	}
	sealed, err := key.Seal([]byte(in.Value))
	if err != nil {
		return "", err
	}

	rel := ssot.SecretFile(in.Name)
	var revision string
	revision, err = w.store.Update(in.Author, func(tx *ssot.Tx) (string, error) {
		if err := tx.EnsureRevision(in.BaseRevision); err != nil {
			return "", err
		}
		existing, err := tx.ReadFile(rel)
		var prev secretFile
		if err == nil {
			if uerr := json.Unmarshal(existing, &prev); uerr != nil {
				return "", fmt.Errorf("解析已有密钥失败: %w", uerr)
			}
		} else if !errors.Is(err, ssot.ErrNotFound) {
			return "", err
		}

		f := secretFile{
			V:         secretFileVersion,
			Name:      in.Name,
			Notes:     in.Notes,
			Refs:      normalizeRefs(in.Refs),
			UpdatedAt: time.Now().UTC(),
			Sealed:    sealed,
			Devices:   prev.Devices,
		}
		if f.Notes == "" {
			f.Notes = prev.Notes
		}
		if len(f.Refs) == 0 {
			f.Refs = prev.Refs
		}
		msg := in.Message
		if msg == "" {
			if len(existing) == 0 {
				msg = fmt.Sprintf("写入密钥 %s", in.Name)
			} else {
				msg = fmt.Sprintf("更新密钥 %s", in.Name)
			}
		}
		return msg, tx.WriteJSON(rel, f)
	})
	if err != nil {
		return "", err
	}
	return revision, nil
}

// GetSecret 解密并返回明文。调用方必须把返回的字节清零。
func (w *Writer) GetSecret(key *crypt.Key, name string) ([]byte, error) {
	if key == nil {
		return nil, errors.New("根密钥未加载")
	}
	if err := ssot.ValidateItemName(name); err != nil {
		return nil, err
	}
	f, err := ssot.ReadJSON[secretFile](w.store, ssot.SecretFile(name))
	if err != nil {
		return nil, err
	}
	if f.Sealed == nil {
		return nil, fmt.Errorf("%w: 密钥 %s 没有密文", ssot.ErrNotFound, name)
	}
	return key.Open(f.Sealed)
}

// ListSecrets 返回全部密钥元数据，不含明文。
func (w *Writer) ListSecrets() ([]proto.SecretMeta, error) {
	files, err := w.store.WalkFiles(ssot.DirSecrets)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]proto.SecretMeta, 0, len(files))
	for _, rel := range files {
		data, err := w.store.ReadFile(rel)
		if err != nil {
			return nil, err
		}
		var f secretFile
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", rel, err)
		}
		if f.Name == "" {
			f.Name = strings.TrimSuffix(path.Base(rel), ".json")
		}
		out = append(out, proto.SecretMeta{
			Name:      f.Name,
			Notes:     f.Notes,
			Refs:      f.Refs,
			UpdatedAt: f.UpdatedAt,
			Devices:   f.Devices,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// DeleteSecret 删除一条密钥。
//
// 默认拒绝删除仍被引用的密钥，并列出引用方（需求 8.4）。
// 没有任何第三方能帮忙恢复被删的密钥，所以这里刻意不提供“静默级联”。
func (w *Writer) DeleteSecret(in PutSecretInput, force bool) (string, error) {
	if err := ssot.ValidateItemName(in.Name); err != nil {
		return "", err
	}
	// 引用可能来自资产的正文，而不只是 refs 元数据，所以先扫一遍全部 bundle。
	refs, err := w.findAssetRefs(in.Name)
	if err != nil {
		return "", err
	}
	if len(refs) > 0 && !force {
		return "", fmt.Errorf("%w: 密钥 %s 仍被 %s 引用", ErrInUse, in.Name, strings.Join(refs, ", "))
	}

	rel := ssot.SecretFile(in.Name)
	return w.store.Update(in.Author, func(tx *ssot.Tx) (string, error) {
		if err := tx.EnsureRevision(in.BaseRevision); err != nil {
			return "", err
		}
		if _, err := tx.ReadFile(rel); err != nil {
			if errors.Is(err, ssot.ErrNotFound) {
				return "", fmt.Errorf("%w: 密钥 %s", ssot.ErrNotFound, in.Name)
			}
			return "", err
		}
		msg := in.Message
		if msg == "" {
			msg = fmt.Sprintf("删除密钥 %s", in.Name)
		}
		return msg, tx.DeleteFile(rel)
	})
}

// findAssetRefs 找出引用了某个密钥的全部资产条目。
func (w *Writer) findAssetRefs(secret string) ([]string, error) {
	bundles, err := w.ListBundles()
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, bundle := range bundles {
		files, err := w.ReadBundleFiles(bundle)
		if err != nil {
			if errors.Is(err, ssot.ErrNotFound) {
				continue
			}
			return nil, err
		}
		for _, f := range files {
			for _, name := range ExtractSecretRefs(f.Content) {
				if name == secret {
					refs = append(refs, bundle+"/"+f.Path)
					break
				}
			}
		}
	}
	sort.Strings(refs)
	return refs, nil
}

// RecordDownload 记录一条密钥下发到了哪些设备，用于吊销后的轮换清单。
func (w *Writer) RecordDownload(key *crypt.Key, secret, deviceID string) error {
	if err := ssot.ValidateItemName(secret); err != nil {
		return err
	}
	if deviceID == "" {
		return nil
	}
	rel := ssot.SecretFile(secret)
	_, err := w.store.Update(ssot.Author{Name: "cloud", Source: "deliver"}, func(tx *ssot.Tx) (string, error) {
		data, err := tx.ReadFile(rel)
		if err != nil {
			return "", err
		}
		var f secretFile
		if err := json.Unmarshal(data, &f); err != nil {
			return "", err
		}
		for _, d := range f.Devices {
			if d == deviceID {
				return "", nil // 无变更，不提交
			}
		}
		f.Devices = append(f.Devices, deviceID)
		sort.Strings(f.Devices)
		return "", tx.WriteJSON(rel, f)
	})
	return err
}

// SecretsDeliveredTo 返回曾经下发到某台设备的密钥名（需求 18.11 的基础数据）。
func (w *Writer) SecretsDeliveredTo(deviceID string) ([]string, error) {
	metas, err := w.ListSecrets()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range metas {
		for _, d := range m.Devices {
			if d == deviceID {
				out = append(out, m.Name)
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// Rekey 在根密钥轮换后，用新根密钥重新加密全部密钥。
//
// 旧密文保留在 git 历史里，因此这里只改当前版本。
func (w *Writer) Rekey(oldKey, newKey *crypt.Key, author ssot.Author) (int, error) {
	files, err := w.store.WalkFiles(ssot.DirSecrets)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	_, err = w.store.Update(author, func(tx *ssot.Tx) (string, error) {
		for _, rel := range files {
			data, err := tx.ReadFile(rel)
			if err != nil {
				return "", err
			}
			var f secretFile
			if err := json.Unmarshal(data, &f); err != nil {
				return "", fmt.Errorf("解析 %s 失败: %w", rel, err)
			}
			if f.Sealed == nil || f.Sealed.KeyID == newKey.ID() {
				continue
			}
			resealed, err := newKey.Rekey(oldKey, f.Sealed)
			if err != nil {
				return "", fmt.Errorf("重新加密 %s 失败: %w", rel, err)
			}
			f.Sealed = resealed
			if err := tx.WriteJSON(rel, f); err != nil {
				return "", err
			}
			count++
		}
		if count == 0 {
			return "", nil
		}
		return "轮换根密钥后重新加密全部密钥", nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// secretNamesFromStore 返回密钥名列表。
func secretNamesFromStore(store *ssot.Store) (int, []string, error) {
	files, err := store.WalkFiles(ssot.DirSecrets)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return 0, nil, nil
		}
		return 0, nil, err
	}
	names := make([]string, 0, len(files))
	for _, rel := range files {
		names = append(names, strings.TrimSuffix(path.Base(rel), ".json"))
	}
	sort.Strings(names)
	return len(names), names, nil
}

// secretNamesTx 在事务内取密钥名，用于写入时的二次复核。
func secretNamesTx(tx *ssot.Tx) ([]string, error) {
	_, names, err := secretNamesFromStore(tx.Store())
	return names, err
}

// normalizeRefs 去重并排序引用关系，保证提交内容稳定。
func normalizeRefs(refs []proto.SecretRef) []proto.SecretRef {
	if len(refs) == 0 {
		return nil
	}
	seen := map[proto.SecretRef]bool{}
	out := make([]proto.SecretRef, 0, len(refs))
	for _, r := range refs {
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bundle != out[j].Bundle {
			return out[i].Bundle < out[j].Bundle
		}
		return out[i].Item < out[j].Item
	})
	return out
}
