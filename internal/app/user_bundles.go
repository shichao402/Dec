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

// ensureVaultBundlesForUserEnable 是用户平面启用路径上的 RepairOn*：
// 已有 vault 包但 scope≠user 时升级为 user；缺包时创建占位并 push。
// 返回本次新建的 bundle 名（仅创建，不含仅升级 scope 的）。
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
		return nil, fmt.Errorf("仓库未连接，无法创建个人 bundle")
	}

	tx, err := repo.NewWriteTransaction()
	if err != nil {
		return nil, err
	}
	defer tx.Close()

	created := make([]string, 0)
	updated := make([]string, 0)
	for _, name := range names {
		manifestRel := types.VaultBundleManifestPath(name)
		manifestAbs := filepath.Join(tx.WorkDir(), filepath.FromSlash(manifestRel))
		if data, readErr := os.ReadFile(manifestAbs); readErr == nil {
			b, parseErr := yamlBundleNameScope(data)
			if parseErr == nil && b.Scope != types.BundleScopeUser {
				b.Scope = types.BundleScopeUser
				if writeErr := writeBundleManifest(manifestAbs, b); writeErr != nil {
					return nil, writeErr
				}
				updated = append(updated, name)
				emit(reporter, EventInfo, "settings.vault", fmt.Sprintf("已将仓库 bundle %q 设为个人范围", name), nil)
			}
			continue
		} else if !os.IsNotExist(readErr) {
			return nil, fmt.Errorf("检查 vault bundle %q 失败: %w", name, readErr)
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
		emit(reporter, EventInfo, "settings.vault", fmt.Sprintf("已在仓库创建占位 bundle %q", name), nil)
	}

	if len(created) == 0 && len(updated) == 0 {
		return nil, nil
	}

	parts := make([]string, 0, 2)
	if len(created) > 0 {
		parts = append(parts, "add placeholders: "+strings.Join(created, ", "))
	}
	if len(updated) > 0 {
		parts = append(parts, "mark scope=user: "+strings.Join(updated, ", "))
	}
	msg := "chore(bundles): user-plane repair — " + strings.Join(parts, "; ")
	if _, err := tx.CommitAndPush(msg); err != nil {
		return nil, fmt.Errorf("推送个人 bundle 修复失败: %w", err)
	}
	_ = secrets.RememberSecretBundles(append(append([]string{}, created...), updated...))
	return created, nil
}

// yamlBundleNameScope 解析已有 manifest；保留 members/description，只规范化 name/scope。
func yamlBundleNameScope(data []byte) (types.Bundle, error) {
	var raw types.Bundle
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return types.Bundle{}, err
	}
	raw.Name = strings.TrimSpace(raw.Name)
	scope := types.BundleScope(strings.TrimSpace(string(raw.Scope)))
	switch scope {
	case types.BundleScopeUser, types.BundleScopeProject:
		raw.Scope = scope
	case "":
		raw.Scope = types.BundleScopeProject
	default:
		return types.Bundle{}, fmt.Errorf("bundle 范围 %q 无效（仅允许 project 或 user）", raw.Scope)
	}
	return raw, nil
}

func writeBundleManifest(path string, b types.Bundle) error {
	body, err := yaml.Marshal(b)
	if err != nil {
		return err
	}
	header := "# Dec bundle\n"
	return os.WriteFile(path, append([]byte(header), body...), 0644)
}

func bundleOverviewNames(bundles []BundleOverview) []string {
	names := make([]string, 0, len(bundles))
	for _, bo := range bundles {
		names = append(names, bo.Name)
	}
	return names
}
