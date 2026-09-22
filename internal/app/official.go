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

// resolvedRequires 是这次要执行的消费声明。已发布项目上的 vault pin 收成 latest，
// 不写盘；写盘由 persistFoldedRequires 负责。
func resolvedRequires(ctx context.Context, workspace Workspace, cfg *types.ProjectConfig) (types.RequiresSpec, string) {
	raw, url := workspaceOfficialRequires(workspace, cfg)
	published := publishedNameSet(officialPublishedVersions(ctx, url))
	return raw.FoldPublishedVaultPins(published, homeProjectName(workspace, cfg)), url
}

func installOfficialRequires(ctx context.Context, workspace Workspace, cfg *types.ProjectConfig, reporter Reporter) ([]install.Resolved, error) {
	req, url := resolvedRequires(ctx, workspace, cfg)
	req = req.Official()
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

// OfficialRequireStatus 是 Console「官方依赖」面板的一行。
type OfficialRequireStatus struct {
	Project             string
	Want                string
	Installed           string
	Available           string
	Tag                 string
	UpdateAvailable     bool
	Error               string
	OverrideActive      bool
	OverrideReadyToDrop bool
}

type OfficialRequiresState struct {
	Items []OfficialRequireStatus
}

// ListOfficialRequires 对照本机 cache 与远端 registry，不安装。
func ListOfficialRequires(ctx context.Context, workspace Workspace) (*OfficialRequiresState, error) {
	cfg, err := loadWorkspaceBundleConfig(workspace)
	if err != nil {
		return nil, err
	}
	req, url := resolvedRequires(ctx, workspace, cfg)
	req = req.Official()
	items, err := install.Status(ctx, install.Options{
		CacheDir:    workspaceCacheDir(workspace),
		RegistryURL: url,
		GitToken:    os.Getenv("DEC_REGISTRY_TOKEN"),
		Requires:    req,
	})
	if err != nil {
		return nil, err
	}
	out := &OfficialRequiresState{Items: make([]OfficialRequireStatus, 0, len(items))}
	for _, item := range items {
		overrideActive, overrideReady := officialOverrideStatus(ctx, workspace, item.Project, item.Available)
		out.Items = append(out.Items, OfficialRequireStatus{
			Project:             item.Project,
			Want:                item.Want,
			Installed:           item.Installed,
			Available:           item.Available,
			Tag:                 item.Tag,
			UpdateAvailable:     item.UpdateAvailable,
			Error:               item.Error,
			OverrideActive:      overrideActive,
			OverrideReadyToDrop: overrideReady,
		})
	}
	return out, nil
}

// UpdateOfficialRequires 只更新用户选中的官方依赖，不触碰个人私仓或 Bitwarden。
func UpdateOfficialRequires(ctx context.Context, workspace Workspace, projects []string, reporter Reporter) (*PullProjectAssetsResult, error) {
	cfg, err := loadWorkspaceBundleConfig(workspace)
	if err != nil {
		return nil, err
	}
	if _, foldErr := persistFoldedRequires(ctx, workspace, cfg, reporter); foldErr != nil {
		emit(reporter, EventWarn, "requires.fold", foldErr.Error(), nil)
	}
	all, url := resolvedRequires(ctx, workspace, cfg)
	all = all.Official()
	selected := make(types.RequiresSpec)
	for _, project := range projects {
		project = strings.TrimSpace(project)
		if want, ok := all[project]; ok {
			selected[project] = want
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("没有选中有效的官方依赖")
	}

	ideSelection, err := config.ResolveEffectiveIDEs(cfg)
	if err != nil {
		return nil, fmt.Errorf("解析有效 IDE 失败: %w", err)
	}
	projectIDEs := uniqueWorkspaceIDEs(workspace, ideSelection.IDEs)
	result := &PullProjectAssetsResult{
		ProjectRoot:   workspace.Root,
		AssetSources:  make(map[string][]string),
		EffectiveIDEs: projectIDENames(projectIDEs),
		IDEWarnings:   append([]string(nil), ideSelection.Warnings...),
	}
	beforeInstall := officialCacheInventory(workspace, selected)
	resolved, err := install.Official(ctx, install.Options{
		CacheDir:    workspaceCacheDir(workspace),
		RegistryURL: url,
		GitToken:    os.Getenv("DEC_REGISTRY_TOKEN"),
		Requires:    selected,
	})
	if err != nil {
		return nil, err
	}
	for _, item := range resolved {
		if err := dropResolvedOverrides(ctx, workspace, item.Project, item.Version); err != nil {
			return nil, err
		}
	}
	if err := renderOfficialFromCache(workspace, selected, projectIDEs, result, reporter); err != nil {
		return nil, err
	}
	pruneRemovedOfficialAssets(workspace, beforeInstall, selected, projectIDEs, result, reporter)
	for _, item := range resolved {
		result.RequiredProjects = appendUniqueSource(result.RequiredProjects, item.Project)
		if item.Warning != "" {
			result.NonFatalWarnings = append(result.NonFatalWarnings, item.Warning)
		}
		emit(reporter, EventInfo, "update.official", fmt.Sprintf("%s@%s", item.Project, item.Version), nil)
	}
	return result, nil
}

// officialCacheInventory 记录安装前 cache 里有哪些官方资产。安装会整目录重写 cache，
// 上游删掉的资产随之消失，届时已经无从判断它曾经被渲染进 IDE。
func officialCacheInventory(workspace Workspace, req types.RequiresSpec) map[string]install.CacheAsset {
	if len(req) == 0 {
		return nil
	}
	cache := workspaceCacheDir(workspace)
	out := make(map[string]install.CacheAsset)
	for project := range req {
		assets, err := install.ListCache(cache, project)
		if err != nil {
			continue
		}
		for _, asset := range assets {
			out[asset.Project+"/"+asset.Type+"/"+asset.Name] = asset
		}
	}
	return out
}

// pruneRemovedOfficialAssets 把上游已删的官方资产从 IDE 里摘掉。
// 不这样做，消费方无论 pull 多少次都清不掉一份提供方早已下架的 Skill。
func pruneRemovedOfficialAssets(workspace Workspace, before map[string]install.CacheAsset, req types.RequiresSpec, projectIDEs []ide.IDE, result *PullProjectAssetsResult, reporter Reporter) {
	if len(before) == 0 {
		return
	}
	after := officialCacheInventory(workspace, req)
	for key, asset := range before {
		if _, ok := after[key]; ok {
			continue
		}
		for _, ideImpl := range projectIDEs {
			_, bounced, _ := removeAssetFromIDE(asset.Type, asset.Name, workspace, ideImpl)
			if bounced != "" {
				result.McpReload = appendUniqueSorted(result.McpReload, bounced)
			}
		}
		line := fmt.Sprintf("[%-5s] %s (项目: %s, 上游已删)", asset.Type, asset.Name, asset.Project)
		result.CleanedAssets = appendUniqueSorted(result.CleanedAssets, line)
		emit(reporter, EventInfo, "pull.cleanup", line, nil)
	}
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
			source := asset.Path
			if override := activeOverrideSource(workspace, asset); override != "" {
				source = override
			}
			bounced, err := installAssetToIDEs(asset.Type, asset.Name, asset.Project, source, workspace, projectIDEs)
			if err != nil {
				result.FailedCount++
				emit(reporter, EventWarn, "install.official", fmt.Sprintf("渲染 %s/%s 失败: %v", asset.Project, asset.Name, err), nil)
				continue
			}
			for _, name := range bounced {
				result.McpReload = appendUniqueSorted(result.McpReload, name)
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

// filterOfficialVaultAssets 从可写 Git 资产里剔除官方 registry 订阅（latest/v*）。
// req 必须已经收过已发布项目的 vault pin；那些由提供方 CI 发布，禁止本机 push。
// 注册表未发布的个人项目仍是 vault pin，保留。
func filterOfficialVaultAssets(req types.RequiresSpec, assets []types.TypedAssetRef) []types.TypedAssetRef {
	official := req.Official()
	if len(official) == 0 {
		return assets
	}
	out := make([]types.TypedAssetRef, 0, len(assets))
	for _, asset := range assets {
		if official.Has(asset.Vault) {
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
