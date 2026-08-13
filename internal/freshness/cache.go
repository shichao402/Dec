package freshness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CachedResult 是后台子进程把 fetch + 对比结果落到磁盘的 JSON 表示。
//
// 主命令 PreRun 只读不写；写入发生在 `dec __freshness-check` 子进程里。
type CachedResult struct {
	LocalCommit  string    `json:"local_commit"`
	RemoteCommit string    `json:"remote_commit"`
	CheckedAt    time.Time `json:"checked_at"`
	Err          string    `json:"error,omitempty"`
}

// IsStale 判断这份缓存是否表示项目资产已落后远端。
//
// 当两边 commit 都非空且不相等才算 stale。空 commit（fetch 失败 / 项目没 pull 过）
// 一律不视为 stale，保守地不打扰用户。
func (r CachedResult) IsStale() bool {
	return r.LocalCommit != "" && r.RemoteCommit != "" && r.LocalCommit != r.RemoteCommit
}

// CacheFilePath 返回项目专属的 cache 文件路径。
// 形如 ~/.dec/local/freshness-result.<sha1-of-abs-path>.json
func CacheFilePath(projectRoot string) (string, error) {
	localDir, hash, err := hashProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(localDir, fmt.Sprintf("freshness-result.%s.json", hash)), nil
}

// ReadCachedResult 读出 cache。
//
// 以下情况统一返回 (nil, nil)：
//   - 文件不存在
//   - 文件损坏（JSON 解析失败）
//   - CheckedAt 距今已超过 Interval()（视为过期）
//
// 这些情况在语义上都代表“这条命令不该根据 cache 提示什么”，
// 所以调用方不必区分，直接看 result 是否为 nil 即可。
func ReadCachedResult(projectRoot string) (*CachedResult, error) {
	path, err := CacheFilePath(projectRoot)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var r CachedResult
	if err := json.Unmarshal(data, &r); err != nil {
		// 坏文件当作没有，避免一次写坏卡住所有后续提示。
		return nil, nil
	}
	if time.Since(r.CheckedAt) >= Interval() {
		return nil, nil
	}
	return &r, nil
}

// WriteCachedResult 原子地把结果写到 cache 文件。
//
// 做法：先写 <path>.tmp，再 rename 覆盖。rename 在同一目录下是原子操作，
// 避免 PreRun 读到半个文件。
func WriteCachedResult(projectRoot string, r CachedResult) error {
	path, err := CacheFilePath(projectRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// InvalidateCache 删除 cache 文件。
//
// `dec pull` 成功后调用，避免旧的 LocalCommit 还躺在 cache 里误触 stale 提示。
// 文件不存在不算错误。
func InvalidateCache(projectRoot string) error {
	path, err := CacheFilePath(projectRoot)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
