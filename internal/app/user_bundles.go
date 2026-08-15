package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

// ensureVaultBundlesForUserEnable 在用户平面勾选启用时尚无 vault 包时创建最小 bundle.yaml 并 push。
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
		return nil, fmt.Errorf("仓库未连接，无法为用户平面启用创建 vault bundle")
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
			Scope:       types.BundleScopeUser,
			Description: "user-plane placeholder (ADR 0009)",
			Members:     []string{},
		})
		if err != nil {
			return nil, err
		}
		header := "# Dec bundle（用户平面启用时自动创建的占位；可后续补充 members）\n"
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
		return nil, fmt.Errorf("推送用户平面 vault bundle 占位失败: %w", err)
	}
	_ = secrets.RememberSecretBundles(created)
	return created, nil
}

func bundleOverviewNames(bundles []BundleOverview) []string {
	names := make([]string, 0, len(bundles))
	for _, bo := range bundles {
		names = append(names, bo.Name)
	}
	return names
}
