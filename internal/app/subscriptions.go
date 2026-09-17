// subscriptions.go 是消费声明（requires）的唯一写入口，见 ADR 0029。
//
// 订阅只写消费方配置：项目平面写 <project>/.dec/config.yaml，Global 平面写
// ~/.dec/config.yaml。个人私仓的项目清单是提供方数据，订阅绝不改写它。
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/install"
	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/registry"
	"github.com/shichao402/Dec/internal/types"
)

// SetRequiresResult 汇报一次订阅保存的结果。
type SetRequiresResult struct {
	ConfigPath string
	VarsPath   string
	// Subscribed 是保存后生效的订阅（项目名 → pin），按项目名排序展示用。
	Subscribed []SubscribedProject
	// Rejected 是被拒绝的订阅及原因，已从 requires 中排除。
	Rejected []string
	// HomeProject 是本工作区的作者身份项目（项目平面才有），它不进 requires。
	HomeProject string
	VarsCreated bool
}

// SubscribedProject 是一条生效的订阅。
type SubscribedProject struct {
	Project string
	Pin     string
	Source  string
}

// SetWorkspaceRequires 把订阅写入所属平面的唯一消费方配置。
//
// spec 为空表示「一项都不订阅」，会清空 requires。私仓 pin 必须在私仓里存在；
// 官方 pin 在能连上注册表时校验项目是否已发布，连不上则接受并告警，不阻断离线编辑。
func SetWorkspaceRequires(ctx context.Context, workspace Workspace, spec types.RequiresSpec, reporter Reporter) (*SetRequiresResult, error) {
	reporter = defaultReporter(reporter)
	normalized, err := types.NormalizeRequiresSpec(spec)
	if err != nil {
		return nil, err
	}

	result := &SetRequiresResult{}
	accepted := make(types.RequiresSpec, len(normalized))
	vaultProjects, vaultErr := scanVaultProjectNames()
	if vaultErr != nil && len(normalized.VaultProjects()) > 0 {
		return nil, vaultErr
	}

	home := ""
	if workspace.EffectivePlane() == WorkspaceProject {
		mgr := config.NewProjectConfigManager(workspace.Root)
		cfg, loadErr := mgr.LoadProjectConfig()
		if loadErr != nil {
			return nil, loadErr
		}
		if cfg != nil {
			home = strings.TrimSpace(cfg.ProjectName)
		}
	}
	result.HomeProject = home

	published := officialPublishedProjects(ctx, reporter)
	for _, name := range sortedRequireNames(normalized) {
		pin := normalized[name]
		switch {
		case name == home:
			result.Rejected = append(result.Rejected, name+"（作者身份项目从工作树创作，不进订阅）")
		case types.IsVaultPin(pin):
			if _, ok := vaultProjects[name]; !ok {
				result.Rejected = append(result.Rejected, name+"（个人私仓里没有这个项目）")
				continue
			}
			accepted[name] = pin
			result.Subscribed = append(result.Subscribed, SubscribedProject{Project: name, Pin: pin, Source: AssetSourceVault})
		default:
			if published != nil {
				if _, ok := published[name]; !ok {
					result.Rejected = append(result.Rejected, name+"（官方注册表还没有发布这个项目）")
					continue
				}
			}
			accepted[name] = pin
			result.Subscribed = append(result.Subscribed, SubscribedProject{Project: name, Pin: pin, Source: AssetSourceOfficial})
		}
	}
	if len(accepted) == 0 {
		accepted = nil
	}

	if workspace.EffectivePlane() == WorkspaceProject {
		mgr := config.NewProjectConfigManager(workspace.Root)
		cfg, err := mgr.LoadProjectConfig()
		if err != nil {
			return nil, err
		}
		if cfg == nil {
			cfg = &types.ProjectConfig{}
		}
		cfg.Requires = accepted
		if err := mgr.SaveProjectConfig(cfg); err != nil {
			return nil, fmt.Errorf("写入项目订阅失败: %w", err)
		}
		varsCreated, err := mgr.EnsureVarsConfigTemplate()
		if err != nil {
			return nil, fmt.Errorf("写入变量定义模板失败: %w", err)
		}
		result.VarsCreated = varsCreated
		result.ConfigPath = filepath.Join(mgr.GetDecDir(), "config.yaml")
		result.VarsPath = mgr.GetVarsPath()
	} else {
		cfg, err := config.LoadGlobalConfig()
		if err != nil {
			return nil, err
		}
		cfg.Requires = accepted
		if err := config.SaveGlobalConfig(cfg); err != nil {
			return nil, fmt.Errorf("写入本机订阅失败: %w", err)
		}
		result.ConfigPath, _ = config.GetGlobalConfigPath()
		result.VarsPath, _ = config.GetGlobalVarsPath()
	}

	for _, rejected := range result.Rejected {
		emit(reporter, EventWarn, "requires.save", "未订阅 "+rejected, nil)
	}
	emit(reporter, EventInfo, "requires.save", fmt.Sprintf("已保存 %d 项订阅", len(result.Subscribed)), nil)
	return result, nil
}

// ListSubscriptionCandidates 是订阅面板的唯一数据源（ADR 0029）：
// 个人私仓项目与官方注册表已发布项目合成一张表，每行带来源与当前 pin。
func ListSubscriptionCandidates(ctx context.Context, workspace Workspace, reporter Reporter) (*AssetSelectionState, error) {
	state, err := LoadWorkspaceAssetSelection(workspace, reporter)
	if err != nil {
		return nil, err
	}
	cfg, err := loadWorkspaceBundleConfig(workspace)
	if err != nil {
		return nil, err
	}
	state.Bundles = mergeOfficialCandidates(state.Bundles, ListOfficialCandidates(ctx, workspace, cfg))
	sortAssetOptions(state.Bundles)
	return state, nil
}

// mergeOfficialCandidates 把官方注册表行并入私仓行，同名项目只留一行。
//
// 合成后的行必须自洽：来源跟着 pin 走。官方 pin 的项目即便私仓里有同名目录，也按官方行下发，
// 否则面板标成「私仓」，还会藏掉已装/可用版本与「有更新」，用户看不到该升级。
// 未被官方 pin 订阅的同名项目保持私仓身份，只补上远端可用版本作为参考。
func mergeOfficialCandidates(vault, official []AssetBundleOption) []AssetBundleOption {
	if len(official) == 0 {
		return vault
	}
	index := make(map[string]int, len(vault))
	for i, opt := range vault {
		index[opt.Name] = i
	}
	for _, opt := range official {
		i, ok := index[opt.Name]
		if !ok {
			vault = append(vault, opt)
			continue
		}
		vault[i].Available = opt.Available
		vault[i].Installed = opt.Installed
		if opt.Pin == "" {
			continue
		}
		vault[i].Source = AssetSourceOfficial
		vault[i].Pin = opt.Pin
		vault[i].Enabled = true
		vault[i].Required = true
		vault[i].UpdateAvailable = opt.UpdateAvailable
	}
	return vault
}

// ListOfficialCandidates 返回官方注册表已发布、且本工作区可订阅的项目行。
// 已订阅项目带上本机已装与远端可用版本，未订阅项目只给可用版本。
func ListOfficialCandidates(ctx context.Context, workspace Workspace, cfg *types.ProjectConfig) []AssetBundleOption {
	requires, url := workspaceOfficialRequires(workspace, cfg)
	published := officialPublishedVersions(ctx, url)
	if len(published) == 0 && len(requires) == 0 {
		return nil
	}
	cacheDir := workspaceCacheDir(workspace)
	names := make([]string, 0, len(published)+len(requires))
	seen := make(map[string]struct{}, len(published)+len(requires))
	for name := range published {
		names = append(names, name)
		seen[name] = struct{}{}
	}
	for _, name := range requires.OfficialProjects() {
		if _, ok := seen[name]; ok {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	options := make([]AssetBundleOption, 0, len(names))
	for _, name := range names {
		pin := ""
		if want, ok := requires[name]; ok && !types.IsVaultPin(want) {
			pin = want
		}
		installed := install.ReadInstalledVersion(cacheDir, name)
		available := published[name]
		opt := AssetBundleOption{
			Name:        name,
			Description: officialCandidateDescription(available),
			Vault:       name,
			Enabled:     pin != "",
			Required:    pin != "",
			Model:       "p",
			Source:      AssetSourceOfficial,
			Pin:         pin,
			Installed:   installed,
			Available:   available,
		}
		if pin != "" && available != "" {
			opt.UpdateAvailable = installed == "" || installed != available
		}
		options = append(options, opt)
	}
	return options
}

func officialCandidateDescription(available string) string {
	if strings.TrimSpace(available) == "" {
		return "官方注册表项目"
	}
	return "官方注册表项目，最新 " + available
}

// officialPublishedVersions 列出注册表里每个项目的最新已发布版本。连不上时返回空表。
func officialPublishedVersions(ctx context.Context, registryURL string) map[string]string {
	url := strings.TrimSpace(registryURL)
	if url == "" {
		url = registry.DefaultURL
	}
	tags, err := registry.ListRemoteTags(ctx, url, registry.TokenEnv(os.Getenv("DEC_REGISTRY_TOKEN")))
	if err != nil {
		return nil
	}
	out := make(map[string]string)
	for _, project := range registry.ProjectsFromTags(tags) {
		versions := registry.VersionsFromTags(project, tags)
		if _, version, resolveErr := registry.Resolve(project, types.RequiresLatest, versions, registry.Yanked{}); resolveErr == nil {
			out[project] = version
		}
	}
	return out
}

func officialPublishedProjects(ctx context.Context, reporter Reporter) map[string]struct{} {
	url := ""
	if g, err := config.LoadGlobalConfig(); err == nil && g != nil {
		url = strings.TrimSpace(g.RegistryURL)
	}
	versions := officialPublishedVersions(ctx, url)
	if len(versions) == 0 {
		emit(reporter, EventWarn, "requires.save", "本次没能列举官方注册表，官方订阅未校验是否已发布", nil)
		return nil
	}
	out := make(map[string]struct{}, len(versions))
	for name := range versions {
		out[name] = struct{}{}
	}
	return out
}

func scanVaultProjectNames() (map[string]struct{}, error) {
	var projects map[string]*pmodel.Loaded
	if err := withLocalReadRepoDir(func(repoDir string) error {
		var err error
		projects, err = pmodel.Scan(repoDir)
		return err
	}); err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(projects))
	for name := range projects {
		out[name] = struct{}{}
	}
	return out, nil
}

// consumedProjectNames 返回本工作区实际消费的项目名：requires 的全部键，
// 加上项目平面的作者身份 home（它的公开资产与 secrets 同样落地本工作区）。
// 它替代了改名前散落各处的 enabled_bundles / enabled_projects（ADR 0029）。
func consumedProjectNames(cfg *types.ProjectConfig, plane WorkspacePlane) []string {
	if cfg == nil {
		return nil
	}
	out := make([]string, 0, len(cfg.Requires)+1)
	seen := make(map[string]struct{}, len(cfg.Requires)+1)
	if plane == WorkspaceProject {
		if home := strings.TrimSpace(cfg.ProjectName); home != "" {
			out = append(out, home)
			seen[home] = struct{}{}
		}
	}
	for _, name := range sortedRequireNames(cfg.Requires) {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func sortedRequireNames(spec types.RequiresSpec) []string {
	names := make([]string, 0, len(spec))
	for name := range spec {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
