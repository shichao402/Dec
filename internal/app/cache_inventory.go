package app

import (
	"strings"

	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/types"
)

func writableProjectNames(workspace Workspace, cfg *types.ProjectConfig) []string {
	if cfg == nil {
		return nil
	}
	if workspace.EffectivePlane() == WorkspaceGlobal {
		return configEnabledPNames(cfg)
	}
	home := strings.TrimSpace(cfg.ProjectName)
	if home == "" {
		return nil
	}
	return []string{home}
}

func scanWritableCacheAssets(workspace Workspace, cfg *types.ProjectConfig) ([]types.TypedAssetRef, error) {
	root := workspaceCacheDir(workspace)
	wantPlane := types.AssetPlaneLocal
	if workspace.EffectivePlane() == WorkspaceGlobal {
		wantPlane = types.AssetPlaneGlobal
	}
	var out []types.TypedAssetRef
	for _, name := range writableProjectNames(workspace, cfg) {
		for _, vis := range []types.AssetVisibility{types.AssetVisibilityPublic, types.AssetVisibilityPrivate} {
			assets, err := pmodel.ScanQuadrant(root, name, vis, wantPlane)
			if err != nil {
				return nil, err
			}
			out = append(out, assets...)
		}
	}
	return out, nil
}

func unionTypedAssets(base, extra []types.TypedAssetRef) []types.TypedAssetRef {
	seen := make(map[string]struct{}, len(base))
	out := append([]types.TypedAssetRef(nil), base...)
	key := func(a types.TypedAssetRef) string {
		return strings.Join([]string{a.Vault, string(a.Visibility), string(types.CanonicalAssetPlane(a.Plane)), a.Type, a.Name}, "\x00")
	}
	for _, a := range base {
		seen[key(a)] = struct{}{}
	}
	for _, a := range extra {
		k := key(a)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, a)
	}
	return out
}
