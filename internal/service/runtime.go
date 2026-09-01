package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

// WriteMetadata 写出发现文件并返回其路径。
//
// 返回路径而不是让调用方稍后重新解析：MetadataPath 依赖 DEC_HOME，进程内该值
// 变化后重新解析会指向别人的文件。服务退出时必须删掉自己写的那一个。
func WriteMetadata(endpoint, token string) (string, error) {
	dir, err := RuntimeDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建服务运行时目录失败: %w", err)
	}
	data, err := json.Marshal(RuntimeMetadata{
		Version:  metadataVersion,
		Endpoint: endpoint,
		Token:    token,
		PID:      os.Getpid(),
	})
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "server-*.json")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	path, err := MetadataPath()
	if err != nil {
		return "", err
	}
	_ = os.Remove(path) // Windows 不允许 Rename 覆盖已存在文件。
	if err := os.Rename(tmpPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func RemoveMetadata() {
	if path, err := MetadataPath(); err == nil {
		RemoveMetadataAt(path)
	}
}

// RemoveMetadataAt 删除指定发现文件，不再经 DEC_HOME 重新解析路径。
func RemoveMetadataAt(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
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

// WaitUntilStopped 等待本机 dec-server 退出。server.lock 是存活权威；
// 抢到锁后顺手清理可能因 Windows 文件竞争而残留的发现文件。
func WaitUntilStopped(ctx context.Context) error {
	deadline := time.Now().Add(15 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	var lastMetadataErr, lastLockErr error
	for {
		if _, err := ReadMetadata(); err != nil {
			lastMetadataErr = err
		} else {
			lastMetadataErr = nil
		}
		lock, err := AcquireServerLock()
		if err == nil {
			RemoveMetadata()
			_ = lock.Unlock()
			return nil
		}
		lastLockErr = err
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 dec-server 退出超时（metadata=%v，lock=%v）", lastMetadataErr, lastLockErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
