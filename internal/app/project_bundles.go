package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

// projectEnableRejection 记录一个不能在项目平面启用的 bundle 及原因。
type projectEnableRejection struct {
	Name   string
	Reason string
}

func projectRejectedNames(rejected []projectEnableRejection) []string {
	names := make([]string, 0, len(rejected))
	for _, rj := range rejected {
		names = append(names, rj.Name)
	}
	return names
}

// validateProjectEnabledBundles 校验项目平面的勾选：bundle 必须在 vault 里本平面可见。
//
// 与用户平面不同，这里只校验、不修复：project bundle 由 vault 显式维护，勾选不该
// 创建占位，也不该改写别人的 scope（ADR 0013）。仓库未连接时无从校验，沿用旧行为
// 直接放行，避免离线时保存不了。
func validateProjectEnabledBundles(names []string, reporter Reporter) ([]projectEnableRejection, error) {
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
		return nil, nil
	}

	tx, err := repo.NewLocalReadTransaction()
	if err != nil {
		return nil, err
	}
	defer tx.Close()

	resolved, err := resolveDesiredAssetsForPlane(nil, tx.WorkDir(), WorkspaceProject, nil)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]struct{}, len(resolved.Bundles))
	for _, bo := range resolved.Bundles {
		visible[bo.Name] = struct{}{}
	}

	var rejected []projectEnableRejection
	for _, name := range names {
		if _, ok := visible[name]; ok {
			continue
		}
		rejected = append(rejected, projectEnableRejection{
			Name:   name,
			Reason: projectEnableRejectReason(tx.WorkDir(), name),
		})
	}
	for _, rj := range rejected {
		emit(reporter, EventWarn, "assets.save",
			fmt.Sprintf("bundle %q 不能在项目平面启用：%s", rj.Name, rj.Reason), nil)
	}
	return rejected, nil
}

func projectEnableRejectReason(repoDir, name string) string {
	manifestAbs := filepath.Join(repoDir, filepath.FromSlash(types.VaultBundleManifestPath(name)))
	data, err := os.ReadFile(manifestAbs)
	if err != nil {
		return "仓库里没有这个 bundle（可能已被删除）"
	}
	b, _, parseErr := yamlBundleNameScope(data)
	if parseErr != nil {
		return "manifest 无法解析: " + parseErr.Error()
	}
	if b.Scope == types.BundleScopeUser {
		return "属于用户平面（scope: user），请在 dec --user 的 Bundles 页启用"
	}
	return "仓库里没有这个 bundle（可能已被删除）"
}
