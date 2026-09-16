package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/contribute"
	"github.com/shichao402/Dec/internal/ide"
	"github.com/shichao402/Dec/internal/install"
	"github.com/shichao402/Dec/internal/types"
)

func workspaceOfficialRequires(workspace Workspace, cfg *types.ProjectConfig) (types.RequiresSpec, string) {
	if workspace.EffectivePlane() == WorkspaceGlobal {
		g, err := config.LoadGlobalConfig()
		if err != nil || g == nil {
			return nil, ""
		}
		return g.Requires, strings.TrimSpace(g.RegistryURL)
	}
	url := ""
	if g, err := config.LoadGlobalConfig(); err == nil && g != nil {
		url = strings.TrimSpace(g.RegistryURL)
	}
	if cfg == nil {
		return nil, url
	}
	return cfg.Requires, url
}

func installOfficialRequires(ctx context.Context, workspace Workspace, cfg *types.ProjectConfig, reporter Reporter) ([]install.Resolved, error) {
	req, url := workspaceOfficialRequires(workspace, cfg)
	if len(req) == 0 {
		return nil, nil
	}
	emit(reporter, EventInfo, "install.official", "从 Dec registry 安装官方 requires", nil)
	resolved, err := install.Official(ctx, install.Options{
		CacheDir:    workspaceCacheDir(workspace),
		RegistryURL: url,
		GitToken:    os.Getenv("DEC_REGISTRY_TOKEN"),
		Requires:    req,
	})
	if err != nil {
		return nil, err
	}
	for _, item := range resolved {
		if item.Warning != "" {
			emit(reporter, EventWarn, "install.official", item.Warning, nil)
		} else {
			emit(reporter, EventInfo, "install.official", fmt.Sprintf("%s@%s", item.Project, item.Version), nil)
		}
	}
	return resolved, nil
}

func renderOfficialFromCache(workspace Workspace, req types.RequiresSpec, projectIDEs []ide.IDE, result *PullProjectAssetsResult, reporter Reporter) error {
	if len(req) == 0 {
		return nil
	}
	cache := workspaceCacheDir(workspace)
	for project := range req {
		assets, err := install.ListCache(cache, project)
		if err != nil {
			return err
		}
		for _, asset := range assets {
			if err := installAssetToIDEs(asset.Type, asset.Name, asset.Project, asset.Path, workspace, projectIDEs); err != nil {
				result.FailedCount++
				emit(reporter, EventWarn, "install.official", fmt.Sprintf("渲染 %s/%s 失败: %v", asset.Project, asset.Name, err), nil)
				continue
			}
			result.PulledCount++
			result.RequestedCount++
		}
	}
	return nil
}

func officialGitPushBlocked(cfg *types.ProjectConfig) string {
	if cfg != nil && len(cfg.Provides) > 0 {
		return "官方资产由提供方 CI 发布到 Dec registry（dec-registry publish-provides），禁止本机 Git push"
	}
	return ""
}

func filterOfficialVaultAssets(req types.RequiresSpec, assets []types.TypedAssetRef) []types.TypedAssetRef {
	if len(req) == 0 {
		return assets
	}
	out := make([]types.TypedAssetRef, 0, len(assets))
	for _, asset := range assets {
		if req.Has(asset.Vault) {
			continue
		}
		out = append(out, asset)
	}
	return out
}

type ProposeUpstreamInput struct {
	ProjectRoot string
	OriginRepo  string
	Asset       string
	Title       string
	Body        string
	Diff        string
	Mode        string
	Branch      string
}

func ProposeUpstream(ctx context.Context, in ProposeUpstreamInput) (*contribute.Result, error) {
	return contribute.Propose(ctx, contribute.Options{
		OriginRepo: in.OriginRepo,
		Asset:      in.Asset,
		Title:      in.Title,
		Body:       in.Body,
		Diff:       in.Diff,
		Mode:       contribute.Mode(in.Mode),
		Branch:     in.Branch,
	})
}
