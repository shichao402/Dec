package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
	"github.com/shichao402/Dec/internal/repo"
)

const (
	runtimeDirName  = "run"
	metadataName    = "server.json"
	serverLockName  = "server.lock"
	metadataVersion = 1
)

// RuntimeMetadata 是门面发现本机 dec-server 所需的非敏感运行时信息。
// Token 只用于本机 RPC 鉴权，不是 Bitwarden session。
type RuntimeMetadata struct {
	Version  int    `json:"version"`
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	PID      int    `json:"pid"`
}

func RuntimeDir() (string, error) {
	root, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, runtimeDirName), nil
}

func MetadataPath() (string, error) {
	dir, err := RuntimeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, metadataName), nil
}

func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成服务 token 失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func ReadMetadata() (*RuntimeMetadata, error) {
	path, err := MetadataPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta RuntimeMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("解析服务发现文件失败: %w", err)
	}
	if meta.Version != metadataVersion || meta.Endpoint == "" || meta.Token == "" {
		return nil, fmt.Errorf("服务发现文件无效")
	}
	return &meta, nil
}

func WriteMetadata(endpoint, token string) error {
	dir, err := RuntimeDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建服务运行时目录失败: %w", err)
	}
	data, err := json.Marshal(RuntimeMetadata{
		Version:  metadataVersion,
		Endpoint: endpoint,
		Token:    token,
		PID:      os.Getpid(),
	})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "server-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	path, err := MetadataPath()
	if err != nil {
		return err
	}
	_ = os.Remove(path) // Windows 不允许 Rename 覆盖已存在文件。
	return os.Rename(tmpPath, path)
}

func RemoveMetadata() {
	if path, err := MetadataPath(); err == nil {
		_ = os.Remove(path)
	}
}

// AcquireServerLock 保证同一 DEC_HOME 只有一个 dec-server。
func AcquireServerLock() (*flock.Flock, error) {
	dir, err := RuntimeDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(dir, serverLockName))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("dec-server 已在运行")
	}
	return lock, nil
}
