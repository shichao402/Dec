// Package freshness 为 dec CLI 提供项目资产陈旧度检测。
//
// 它只做三件事：
//   1. 读取项目的 .dec/.version 里上次 pull 时固化的 commit hash
//   2. 从本地 bare repo 拉取最新远端 HEAD commit hash
//   3. 用 ~/.dec/local/last-freshness-check.<hash> 的 mtime 做节流
//
// 该包不会触发 dec pull、不改任何文件（除了 touch 节流文件），
// 也不会把错误冒泡到命令主流程——所有内部错误只代表“本次不提示”。
package freshness

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shichao402/Dec/pkg/repo"
)

// DefaultInterval 默认节流窗口。
const DefaultInterval = 24 * time.Hour

// VersionMeta 对应 .dec/.version 文件内容。
type VersionMeta struct {
	Commit   string
	PulledAt string
}

// LoadVersionMeta 读取 .dec/.version。
//
// 文件不存在时返回 (nil, nil)，表示项目还没执行过 dec pull。
// 格式错误时返回空 Commit 而非报错，调用方应把空 Commit 视为“不可比较”。
func LoadVersionMeta(projectRoot string) (*VersionMeta, error) {
	path := filepath.Join(projectRoot, ".dec", ".version")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	meta := &VersionMeta{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		switch key {
		case "commit":
			meta.Commit = val
		case "pulled_at":
			meta.PulledAt = val
		}
	}
	return meta, nil
}

// FetchRemoteHead 获取远端默认分支的最新 commit hash。
//
// 会阻塞于 git fetch（走 bare repo），调用方必须给出超时 ctx 或在 goroutine 中使用。
// 任何错误（未连接仓库、网络不可达、分支找不到）都返回空字符串 + error，
// 调用方据此决定是否沉默略过。
func FetchRemoteHead() (string, error) {
	connected, err := repo.IsBareConnected()
	if err != nil {
		return "", err
	}
	if !connected {
		return "", fmt.Errorf("仓库未连接")
	}

	if err := repo.FetchBare(); err != nil {
		return "", err
	}

	branch, err := repo.GetDefaultBranch()
	if err != nil {
		return "", err
	}

	bareDir, err := repo.GetBareRepoDir()
	if err != nil {
		return "", err
	}

	cmd := exec.Command("git", "--git-dir", bareDir, "rev-parse", fmt.Sprintf("refs/heads/%s", branch))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("读取 refs/heads/%s 失败: %s", branch, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// StateFilePath 为给定项目根目录返回其专属节流状态文件路径。
//
// 形如 ~/.dec/local/last-freshness-check.<sha1-of-abs-path>
func StateFilePath(projectRoot string) (string, error) {
	localDir, hash, err := hashProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(localDir, fmt.Sprintf("last-freshness-check.%s", hash)), nil
}

// hashProjectRoot 把项目根目录映射到 (~/.dec/local, sha1-of-abs-path)。
//
// 同一项目在多个 per-project 文件（throttle state、cache）间共享同一 hash，
// 保证 local/ 目录下的文件按项目聚合，且跨版本稳定。
func hashProjectRoot(projectRoot string) (localDir string, hash string, err error) {
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", "", err
	}
	rootDir, err := repo.GetRootDir()
	if err != nil {
		return "", "", err
	}
	localDir = filepath.Join(rootDir, "local")
	sum := sha1.Sum([]byte(abs))
	return localDir, hex.EncodeToString(sum[:]), nil
}

// ShouldCheck 判断是否已超过节流窗口，允许再次 fetch 远端。
//
// 状态文件不存在或读取出错时返回 true（首次或不确定时倾向于执行检查）。
func ShouldCheck(stateFile string, interval time.Duration) bool {
	if interval <= 0 {
		return true
	}
	info, err := os.Stat(stateFile)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) >= interval
}

// RecordCheck 更新状态文件 mtime，标记本轮节流已用掉。
//
// 文件不存在会自动创建；父目录不存在会自动 mkdir。
// 任何写失败都会冒泡给调用方，但调用方通常只应记录，不应影响命令退出码。
func RecordCheck(stateFile string) error {
	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		return err
	}
	now := time.Now()
	if f, err := os.OpenFile(stateFile, os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		_ = f.Close()
	} else if !os.IsExist(err) {
		return err
	}
	return os.Chtimes(stateFile, now, now)
}

// ShortHash 返回 commit hash 前 7 位，用于用户可见的提示。
func ShortHash(hash string) string {
	if len(hash) >= 7 {
		return hash[:7]
	}
	return hash
}
