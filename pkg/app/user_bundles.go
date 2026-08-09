package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
	"gopkg.in/yaml.v3"
)

// mergeProjectAndUserEnabledBundles 合并 project 与本机用户级启用（ADR 0003）。
func mergeProjectAndUserEnabledBundles(projectEnabled []string) ([]string, error) {
	cfg, err := secrets.LoadConfig()
	if err != nil {
		return nil, err
	}
	return secrets.MergeEnabledBundles(projectEnabled, cfg.UserEnabledBundleNames()), nil
}

// ensureVaultBundlesForUserEnable 在用户勾选启用时尚无 vault 包时创建最小 bundle.yaml 并 push。
// 返回本次新建的 bundle 名（已存在的跳过）。
func ensureVaultBundlesForUserEnable(names []string, reporter Reporter) ([]string, error) {
	reporter = defaultReporter(reporter)
	names = secrets.NormalizeBundleNames(names)
	if len(names) == 0 {
		return nil, nil
	}

	connected, err := repo.IsConnected()
	if err != nil {
		return nil, fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return nil, fmt.Errorf("仓库未连接，无法为用户级启用创建 vault bundle")
	}

	tx, err := repo.NewWriteTransaction()
	if err != nil {
		return nil, err
	}
	defer tx.Close()

	created := make([]string, 0)
	for _, name := range names {
		manifestRel := types.VaultBundleManifestPath(name)
		manifestAbs := filepath.Join(tx.WorkDir(), filepath.FromSlash(manifestRel))
		if _, err := os.Stat(manifestAbs); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("检查 vault bundle %q 失败: %w", name, err)
		}
		if err := os.MkdirAll(filepath.Dir(manifestAbs), 0755); err != nil {
			return nil, err
		}
		body, err := yaml.Marshal(types.Bundle{
			Name:        name,
			Description: "secrets-only / machine-enabled placeholder (ADR 0003)",
			Members:     []string{},
		})
		if err != nil {
			return nil, err
		}
		header := "# Dec bundle（用户级启用时自动创建的占位；可后续补充 members）\n"
		if err := os.WriteFile(manifestAbs, append([]byte(header), body...), 0644); err != nil {
			return nil, fmt.Errorf("写入 %s 失败: %w", manifestRel, err)
		}
		created = append(created, name)
		emit(reporter, EventInfo, "settings.vault", fmt.Sprintf("已创建 vault 占位 bundle/%s", name), nil)
	}

	if len(created) == 0 {
		return nil, nil
	}

	msg := "chore(bundles): add user-enabled placeholders: " + strings.Join(created, ", ")
	if _, err := tx.CommitAndPush(msg); err != nil {
		return nil, fmt.Errorf("推送用户级 vault bundle 占位失败: %w", err)
	}
	return created, nil
}
