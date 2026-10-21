package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/prune"
	"github.com/shichao402/Dec/internal/repo"
)

// 剪枝隔离区的位置与策略可通过环境变量覆盖，便于用户调整与测试。
const (
	// EnvPruneRetention 覆盖保留期，接受 Go duration（"336h"）或纯小时数（"336"）。
	EnvPruneRetention = "DEC_PRUNE_RETENTION"
	// EnvPruneMaxBytes 覆盖容量上限，接受字节数或带 K/M/G 后缀。
	EnvPruneMaxBytes = "DEC_PRUNE_MAX_BYTES"
	// EnvPruneQuarantineDir 覆盖隔离区根目录，主要用于测试与诊断。
	EnvPruneQuarantineDir = "DEC_PRUNE_QUARANTINE_DIR"
)

// pruneQuarantineRoot 返回隔离区根目录，默认落在 ~/.dec/pruned。
//
// 不放在项目工作区内：本地剪枝可能发生在 user 平面的 ~/.dec/secrets 与
// ~/.ssh，项目内的隔离目录对它们没有意义，且会被误提交进 git。
func pruneQuarantineRoot() (string, error) {
	if custom := strings.TrimSpace(os.Getenv(EnvPruneQuarantineDir)); custom != "" {
		return filepath.Abs(custom)
	}
	rootDir, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "pruned"), nil
}

// pruneQuarantineStore 返回按当前环境配置构造的隔离区句柄。
func pruneQuarantineStore() (*prune.Store, error) {
	root, err := pruneQuarantineRoot()
	if err != nil {
		return nil, err
	}
	opts, err := prune.OptionsFromEnv(os.LookupEnv)
	if err != nil {
		return nil, err
	}
	return prune.New(root, opts), nil
}

// mustPruneQuarantineStore 在隔离区不可用时返回错误，调用方必须据此中止剪枝，
// 禁止在隔离区不可用时退化为永久删除。
func mustPruneQuarantineStore() (*prune.Store, error) {
	store, err := pruneQuarantineStore()
	if err != nil {
		return nil, fmt.Errorf("初始化剪枝隔离区失败: %w", err)
	}
	return store, nil
}
