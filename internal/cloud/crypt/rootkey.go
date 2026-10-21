package crypt

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// 根密钥文件的布局与加载规则（对应需求 9.1 到 9.5）。
//
// 根密钥文件存放在服务端本地，权限 0600，永不进入 git 与冷备。
// 服务启动时自动加载并校验；缺失或校验失败时以恢复模式启动，
// 通过登记/恢复接口从 Bitwarden 冷备取回后写回本地文件。

// FileMode 是根密钥文件的权限。
const FileMode fs.FileMode = 0o600

var (
	// ErrRootKeyMissing 表示本地根密钥文件不存在。
	ErrRootKeyMissing = errors.New("root key file missing")
	// ErrRootKeyCorrupt 表示文件存在但无法解析，或与 git 内校验值不一致。
	ErrRootKeyCorrupt = errors.New("root key file corrupt")
)

// rootKeyFile 是根密钥文件的 JSON 结构。
//
// Fingerprint 是 Commit 里记录的校验值来源：服务端把 key id 与创建时间写进 git，
// 启动时核对本地文件的 key id 与之匹配，防止误换了另一份根密钥而静默解密失败。
type rootKeyFile struct {
	V           int       `json:"v"`
	KeyID       string    `json:"key_id"`
	Material    string    `json:"material"`
	CreatedAt   time.Time `json:"created_at"`
	RotatedFrom string    `json:"rotated_from,omitempty"`
}

const rootKeyFileVersion = 1

// LoadRootKey 从 path 读取根密钥。
//
// 返回 ErrRootKeyMissing 表示首次启动或文件被误删，需要走恢复流程；
// 返回 ErrRootKeyCorrupt 表示文件损坏，同样需要恢复，且必须告警。
func LoadRootKey(path string) (*Key, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrRootKeyMissing, path)
		}
		return nil, fmt.Errorf("读取根密钥文件失败: %w", err)
	}
	// 覆盖权限被放宽的情况，避免随后启动仍沿用宽权限。
	if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(path, FileMode)
	}

	var f rootKeyFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRootKeyCorrupt, err)
	}
	if f.V != rootKeyFileVersion {
		return nil, fmt.Errorf("%w: 不支持的文件版本 %d", ErrRootKeyCorrupt, f.V)
	}
	material, err := hex.DecodeString(f.Material)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRootKeyCorrupt, err)
	}
	defer wipe(material)

	key, err := KeyFromBytes(f.KeyID, material)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRootKeyCorrupt, err)
	}
	return key, nil
}

// WriteRootKey 原子地写入根密钥文件，权限 0600。
// 先写同目录临时文件并 fsync，再 rename，避免断电留下半个文件。
func WriteRootKey(path string, key *Key, createdAt time.Time, rotatedFrom string) error {
	if key == nil {
		return errors.New("根密钥为空")
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	material := key.Material()
	defer wipe(material)

	f := rootKeyFile{
		V:           rootKeyFileVersion,
		KeyID:       key.ID(),
		Material:    hex.EncodeToString(material),
		CreatedAt:   createdAt.UTC(),
		RotatedFrom: rotatedFrom,
	}
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("序列化根密钥失败: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建根密钥目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".rootkey-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(FileMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("设置权限失败: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入根密钥失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("同步根密钥失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换根密钥文件失败: %w", err)
	}
	return nil
}

// ParseRootKeyPayload 解析从 Bitwarden 冷备取回的根密钥内容。
//
// 冷备条目的内容与本地文件同构，这样初始化时“写冷备”和“恢复时读冷备”走同一套格式，
// 读回比对也能直接比较字节。
func ParseRootKeyPayload(data []byte) (*Key, time.Time, string, error) {
	var f rootKeyFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, time.Time{}, "", fmt.Errorf("%w: %v", ErrRootKeyCorrupt, err)
	}
	if f.V != rootKeyFileVersion {
		return nil, time.Time{}, "", fmt.Errorf("%w: 不支持的文件版本 %d", ErrRootKeyCorrupt, f.V)
	}
	material, err := hex.DecodeString(f.Material)
	if err != nil {
		return nil, time.Time{}, "", fmt.Errorf("%w: %v", ErrRootKeyCorrupt, err)
	}
	defer wipe(material)
	key, err := KeyFromBytes(f.KeyID, material)
	if err != nil {
		return nil, time.Time{}, "", fmt.Errorf("%w: %v", ErrRootKeyCorrupt, err)
	}
	return key, f.CreatedAt, f.RotatedFrom, nil
}

// RootKeyPayload 生成可写入 Bitwarden 冷备的内容，与本地文件字节一致。
func RootKeyPayload(key *Key, createdAt time.Time, rotatedFrom string) ([]byte, error) {
	if key == nil {
		return nil, errors.New("根密钥为空")
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	material := key.Material()
	defer wipe(material)
	return json.Marshal(rootKeyFile{
		V:           rootKeyFileVersion,
		KeyID:       key.ID(),
		Material:    hex.EncodeToString(material),
		CreatedAt:   createdAt.UTC(),
		RotatedFrom: rotatedFrom,
	})
}
