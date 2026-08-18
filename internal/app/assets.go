package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

// AssetSelectionItem 描述 bundle 内的一个成员资产，仅供展示定位使用。
type AssetSelectionItem struct {
	Name  string
	Type  string
	Vault string
}

// AssetBundleOption 描述 Bundles 页可勾选的 bundle 节点。
//
// 字段含义与 app.BundleOverview 一致，额外把成员展开成 AssetSelectionItem 级别的定位信息
// （Type + Vault + Name），便于 TUI 渲染成员列表。
type AssetBundleOption struct {
	Name        string
	Description string
	Vault       string
	// Members 为 bundle 成员解析后的定位信息，顺序与 bundle YAML 中声明保持一致。
	// 若成员解析失败或资产不存在，这里会跳过（LoadAssetSelection 已通过 reporter 打 warning）。
	Members []AssetSelectionItem
	// Enabled 表示当前平面的 enabled_bundles 是否已引用该 bundle。
	Enabled bool
	// SecretsOnly 表示该 bundle 目前只存在于 Bitwarden / known 列表，vault 里还没有 manifest。
	// 勾选保存后 ensureVaultBundlesForUserEnable 会补一份 scope=user 的 manifest，此标记随之消失。
	// 仅用户平面会出现：这是把纯 secrets bundle 提升为 user bundle 的唯一入口。
	SecretsOnly bool
	// OtherPlane 表示 vault 里有该 bundle 的 manifest，但 scope 属于另一平面。
	// 它不是「未登记」，也不能在本平面启用——跨平面要先显式改 manifest 的 scope（ADR 0013）。
	OtherPlane bool
	// RemoteMissing 表示本次核对过 Bitwarden，远端并没有同名 secrets folder：
	// 该候选纯粹来自本机 known_secret_bundles / enabled_bundles 的残留记录。
	// 仅在 SecretsOnly 为 true 时有意义，用于避免谎称「Bitwarden 已有同名 secrets」。
	RemoteMissing bool
	// RemoteUnverified 表示本次没能核对远端（无 session、枚举失败，或该 bundle 配了别名 folder），
	// 因此既不能声称远端已有、也不能断言远端没有。仅在 SecretsOnly 为 true 时有意义。
	RemoteUnverified bool
}

// AssetSelectionState 是 Bundles 页的数据源：仓库里全部 bundle + 当前启用态。
type AssetSelectionState struct {
	ProjectRoot    string
	ConfigPath     string
	VarsPath       string
	ExistingConfig bool
	VarsFileReady  bool
	// Bundles 是当前仓库扫描得到的全部 bundle 选项，含未启用的。
	// 仓库未连接或扫描失败时为 nil（调用方应当作"没有 bundle"处理）。
	Bundles []AssetBundleOption
}

// SaveBundleSelectionResult 汇报一次 bundle 勾选保存的结果。
type SaveBundleSelectionResult struct {
	ConfigPath         string
	VarsPath           string
	VarsCreated        bool
	EnabledBundleCount int
	// RejectedBundles 是本次勾选中未能启用的条目（含原因），已从 enabled_bundles 排除。
	// 两平面都会出现：跨平面启用需先显式改 vault manifest 的 scope（ADR 0013 §7 / §7a）。
	RejectedBundles []string
}

func LoadAssetSelection(projectRoot string, reporter Reporter) (*AssetSelectionState, error) {
	return LoadWorkspaceAssetSelection(NewWorkspace(WorkspaceProject, projectRoot), reporter)
}

// LoadWorkspaceAssetSelection 加载指定平面可见且可启用的 bundle。
func LoadWorkspaceAssetSelection(workspace Workspace, reporter Reporter) (*AssetSelectionState, error) {
	reporter = defaultReporter(reporter)
	connected, err := repo.IsConnected()
	if err != nil {
		return nil, fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return nil, fmt.Errorf("仓库未连接\n\n在 Settings 页填写 Dec 仓库地址后重试")
	}

	projectRoot := workspace.Root
	mgr := config.NewProjectConfigManager(projectRoot)
	state := &AssetSelectionState{
		ProjectRoot: projectRoot,
		ConfigPath:  filepath.Join(mgr.GetDecDir(), "config.yaml"),
		VarsPath:    mgr.GetVarsPath(),
	}

	var existingConfig *types.ProjectConfig
	if workspace.EffectivePlane() == WorkspaceUser {
		globalConfig, loadErr := config.LoadGlobalConfig()
		if loadErr != nil {
			return nil, loadErr
		}
		globalPath, pathErr := config.GetGlobalConfigPath()
		if pathErr != nil {
			return nil, pathErr
		}
		state.ConfigPath = globalPath
		state.VarsPath, _ = config.GetGlobalVarsPath()
		state.ExistingConfig = true
		existingConfig = &types.ProjectConfig{EnabledBundles: append([]string(nil), globalConfig.EnabledBundles...)}
	} else if mgr.Exists() {
		state.ExistingConfig = true
		emit(reporter, EventInfo, "assets.load", "检测到现有项目配置，准备加载 bundle 选择状态", nil)
		loadedConfig, err := mgr.LoadProjectConfig()
		if err != nil {
			return nil, err
		}
		existingConfig = loadedConfig
	}

	state.Bundles = loadBundleSelectionForPlane(existingConfig, workspace.EffectivePlane(), reporter)
	if workspace.EffectivePlane() == WorkspaceUser {
		state.Bundles = appendSecretsOnlyBundleOptions(state.Bundles, workspace, existingConfig, reporter)
	}

	if _, err := os.Stat(state.VarsPath); err == nil {
		state.VarsFileReady = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查项目变量文件失败: %w", err)
	}

	emit(reporter, EventInfo, "assets.load", fmt.Sprintf("bundle 选择状态已加载，共 %d 个 bundle", len(state.Bundles)), nil)
	return state, nil
}

// SaveEnabledBundles 把 bundle 勾选写入 .dec/config.yaml。
//
// bundles 为 nil 或空表示「一个 bundle 都不启用」，会清空 enabled_bundles。
// 除 enabled_bundles 外的字段（IDEs / Editor / Version / ProjectName）一律从磁盘原样带过。
func SaveEnabledBundles(projectRoot string, bundles []string, reporter Reporter) (*SaveBundleSelectionResult, error) {
	return SaveWorkspaceEnabledBundles(NewWorkspace(WorkspaceProject, projectRoot), bundles, reporter)
}

// SaveWorkspaceEnabledBundles 将选择写入所属平面的唯一配置源。
func SaveWorkspaceEnabledBundles(workspace Workspace, bundles []string, reporter Reporter) (*SaveBundleSelectionResult, error) {
	reporter = defaultReporter(reporter)
	if workspace.EffectivePlane() == WorkspaceUser {
		globalConfig, err := config.LoadGlobalConfig()
		if err != nil {
			return nil, err
		}
		configPath, err := config.GetGlobalConfigPath()
		if err != nil {
			return nil, err
		}
		result := &SaveBundleSelectionResult{ConfigPath: configPath}
		result.VarsPath, _ = config.GetGlobalVarsPath()

		// 先修/校验共享 vault，再落本机启用列表：被拒绝的名字不能进 enabled_bundles，
		// 否则会留下一个「勾了但平面隔离永远看不见」的条目（ADR 0013）。
		requested := normalizeEnabledBundles(bundles)
		emit(reporter, EventInfo, "assets.save", "校验仓库 bundle 声明", &Progress{Phase: "write", Current: 1, Total: 2})
		repair, err := ensureVaultBundlesForUserEnable(requested, reporter)
		if err != nil {
			return nil, err
		}
		if repair != nil && len(repair.Rejected) > 0 {
			requested = excludeBundleNames(requested, repair.rejectedNames())
			for _, rj := range repair.Rejected {
				result.RejectedBundles = append(result.RejectedBundles,
					fmt.Sprintf("%s（%s）", rj.Name, rj.Reason))
			}
		}

		globalConfig.EnabledBundles = requested
		result.EnabledBundleCount = len(requested)
		if err := config.SaveGlobalConfig(globalConfig); err != nil {
			return nil, fmt.Errorf("写入用户平面配置失败: %w", err)
		}
		emit(reporter, EventInfo, "assets.save", "bundle 选择已保存", &Progress{Phase: "write", Current: 2, Total: 2})
		return result, nil
	}
	projectRoot := workspace.Root
	mgr := config.NewProjectConfigManager(projectRoot)
	result := &SaveBundleSelectionResult{
		ConfigPath: filepath.Join(mgr.GetDecDir(), "config.yaml"),
		VarsPath:   mgr.GetVarsPath(),
	}

	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	if projectConfig == nil {
		projectConfig = &types.ProjectConfig{}
	}

	// 与用户平面一样先校验仓库声明：本平面看不见的名字不能进 enabled_bundles，
	// 否则每次 pull 只会得到一句「引用的 bundle 找不到声明，已忽略」。
	requested := normalizeEnabledBundles(bundles)
	emit(reporter, EventInfo, "assets.save", "校验仓库 bundle 声明", nil)
	rejected, err := validateProjectEnabledBundles(requested, reporter)
	if err != nil {
		return nil, err
	}
	if len(rejected) > 0 {
		requested = excludeBundleNames(requested, projectRejectedNames(rejected))
		for _, rj := range rejected {
			result.RejectedBundles = append(result.RejectedBundles,
				fmt.Sprintf("%s（%s）", rj.Name, rj.Reason))
		}
	}

	projectConfig.EnabledBundles = requested
	result.EnabledBundleCount = len(projectConfig.EnabledBundles)

	emit(reporter, EventInfo, "assets.save", "写入项目配置", &Progress{Phase: "write", Current: 1, Total: 2})
	if err := mgr.SaveProjectConfig(projectConfig); err != nil {
		return nil, fmt.Errorf("写入配置失败: %w", err)
	}

	varsCreated, err := mgr.EnsureVarsConfigTemplate()
	if err != nil {
		return nil, fmt.Errorf("写入变量定义模板失败: %w", err)
	}
	result.VarsCreated = varsCreated

	emit(reporter, EventInfo, "assets.save", "bundle 选择已保存", &Progress{Phase: "write", Current: 2, Total: 2})
	return result, nil
}

// loadBundleSelection 扫描仓库内 bundle 声明，返回全部 bundle 选项（含未启用的）。
//
// 本函数只为 Bundles 页展示服务，任何错误都降级为 reporter warning，不向上传播；
// 失败时返回 nil，调用方按"没有 bundle"处理。
func loadBundleSelection(projectConfig *types.ProjectConfig, reporter Reporter) []AssetBundleOption {
	return loadBundleSelectionForPlane(projectConfig, WorkspaceProject, reporter)
}

func loadBundleSelectionForPlane(projectConfig *types.ProjectConfig, plane WorkspacePlane, reporter Reporter) []AssetBundleOption {
	tx, err := repo.NewLocalReadTransaction()
	if err != nil {
		emit(reporter, EventWarn, "assets.bundle",
			fmt.Sprintf("打开仓库只读事务失败，Bundles 页将不展示 bundle: %v", err), nil)
		return nil
	}
	defer tx.Close()

	resolved, err := resolveDesiredAssetsForPlane(projectConfig, tx.WorkDir(), plane, reporter)
	if err != nil {
		emit(reporter, EventWarn, "assets.bundle",
			fmt.Sprintf("解析 bundle 声明失败，Bundles 页将不展示 bundle: %v", err), nil)
		return nil
	}

	if names := bundleOverviewNames(resolved.Bundles); len(names) > 0 {
		_ = secrets.RememberSecretBundles(names)
	}

	enabledSet := make(map[string]struct{})
	if projectConfig != nil {
		for _, name := range projectConfig.EnabledBundles {
			enabledSet[name] = struct{}{}
		}
	}
	options := make([]AssetBundleOption, 0, len(resolved.Bundles))
	for _, bo := range resolved.Bundles {
		opt := AssetBundleOption{
			Name:        bo.Name,
			Description: bo.Description,
			Vault:       bo.VaultName,
			Enabled:     bo.Enabled,
		}
		if _, ok := enabledSet[bo.Name]; ok {
			opt.Enabled = true
		}
		opt.Members = buildBundleMemberItems(bo, tx.WorkDir())
		options = append(options, opt)
	}
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].Name != options[j].Name {
			return options[i].Name < options[j].Name
		}
		return options[i].Vault < options[j].Vault
	})

	return options
}

// appendSecretsOnlyBundleOptions 把只存在于 Bitwarden / known 的 secrets bundle 补进用户平面候选。
//
// vault 扫描只能看到已有 manifest 的 bundle，但用户平面的启用列表同时决定 secrets bundle 的拉取，
// 因此新的 Bitwarden folder 必须能在这里被首次勾选（保存时再补 manifest）。这是 Settings 页
// 移除重复入口后唯一的提升路径，缺了它纯 secrets bundle 将无法启用。
//
// known_secret_bundles 混着两个平面的名字，所以补进来的候选必须再按 vault scope 分流：
// vault 里已有 manifest、只是属于另一平面的，标 OtherPlane 而不是 SecretsOnly——否则会
// 谎称「vault 尚无 manifest」，并诱导用户勾选，进而触发跨平面 scope 改写（ADR 0013）。
//
// known_secret_bundles 是只增不减的本机缓存（只有 pull reconcile / 删除 bundle 才摘），
// 所以还要按远端名单分流：远端已核对且没有同名 folder 的候选是残留记录，不能标成
// 「Bitwarden 已有」——那会诱导用户勾选出一个拉不到任何 secrets 的空 user bundle。
func appendSecretsOnlyBundleOptions(options []AssetBundleOption, workspace Workspace, projectConfig *types.ProjectConfig, reporter Reporter) []AssetBundleOption {
	existing := make(map[string]struct{}, len(options))
	for _, opt := range options {
		existing[opt.Name] = struct{}{}
	}
	scopes := loadVaultBundleScopes(workspace, reporter)

	var enabled []string
	if projectConfig != nil {
		enabled = projectConfig.EnabledBundles
	}
	sessionReady := secrets.HasSession() && secrets.HasUserKey()
	inventory := listRemoteSecretBundleInventory(sessionReady, "assets.bundle", reporter)
	known := []string(nil)
	aliased := map[string]struct{}{}
	if cfg, err := secrets.LoadConfig(); err == nil {
		known = cfg.KnownSecretBundleNames()
		aliased = aliasedSecretBundleNames(cfg)
	}

	enabledSet := make(map[string]struct{}, len(enabled))
	for _, name := range secrets.NormalizeBundleNames(enabled) {
		enabledSet[name] = struct{}{}
	}
	var stale []string
	candidates := secrets.NormalizeBundleNames(append(append(append([]string{}, enabled...), known...), inventory.Names...))
	for _, name := range candidates {
		if _, ok := existing[name]; ok {
			continue
		}
		_, isEnabled := enabledSet[name]
		_, hasAlias := aliased[name]
		opt := AssetBundleOption{Name: name, Enabled: isEnabled}
		switch {
		case scopes.belongsToOtherPlane(name):
			opt.OtherPlane = true
			opt.Description = "属于项目平面（vault manifest 的 scope 不是 user）"
		case inventory.has(name):
			opt.SecretsOnly = true
			opt.Description = "仓库未登记（Bitwarden 已有，vault 尚无 manifest）"
		case !inventory.Checked || hasAlias:
			// 没问过远端，或该 bundle 绑了别名 folder（远端枚举只覆盖 bundle/*），不下结论。
			opt.SecretsOnly = true
			opt.RemoteUnverified = true
			opt.Description = "仓库未登记（本机记录，本次未核对 Bitwarden）"
		case !isEnabled:
			// 远端已核对、既没有 secrets folder 也没有 manifest，且没人在用：直接摘掉本机残留。
			stale = append(stale, name)
			continue
		default:
			opt.SecretsOnly = true
			opt.RemoteMissing = true
			opt.Description = "已启用但远端无内容（Bitwarden 无同名 secrets，vault 也无 manifest）"
		}
		options = append(options, opt)
	}
	forgetStaleKnownSecretBundles(stale, reporter)
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].Name != options[j].Name {
			return options[i].Name < options[j].Name
		}
		return options[i].Vault < options[j].Vault
	})
	return options
}

// aliasedSecretBundleNames 返回配了显式 secrets folder 别名的 bundle 名。
// 远端 bundle 枚举只覆盖 bundle/* folder，别名 bundle 不在其中，
// 不能因为「不在名单里」就判它远端无 secrets。
func aliasedSecretBundleNames(cfg *secrets.Config) map[string]struct{} {
	out := map[string]struct{}{}
	if cfg == nil {
		return out
	}
	for _, b := range cfg.Bundles {
		name := strings.TrimSpace(b.DecBundleName)
		if name == "" {
			continue
		}
		if strings.TrimSpace(b.SecretsBundleName) == secrets.DefaultBundleFolder(name) {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}

// forgetStaleKnownSecretBundles 摘掉远端已不存在、也没人启用的 known_secret_bundles 记录。
// known 是只增不减的缓存，不摘的话这些名字会一直以「Bitwarden 已有」的面目留在候选里。
func forgetStaleKnownSecretBundles(names []string, reporter Reporter) {
	if len(names) == 0 {
		return
	}
	if err := secrets.ForgetSecretBundles(names); err != nil {
		emit(reporter, EventWarn, "assets.bundle",
			fmt.Sprintf("清除 known_secret_bundles 残留失败: %v", err), nil)
		return
	}
	emit(reporter, EventInfo, "assets.bundle",
		fmt.Sprintf("已摘除远端已不存在的本机 bundle 记录: %s", strings.Join(names, ", ")), nil)
}

// buildBundleMemberItems 把 BundleOverview 里的成员引用解析成 AssetSelectionItem。
// Members 使用的是 bundle YAML 原始引用（例如 "skills/vikunja-workflow"），
// 这里只做 ParseMember + 简单 vault 归位，不再做文件存在性校验（resolveDesiredAssets
// 已经在打 warning 时过滤过）。
func buildBundleMemberItems(bo BundleOverview, repoDir string) []AssetSelectionItem {
	items := make([]AssetSelectionItem, 0, len(bo.Members))
	for _, raw := range bo.Members {
		parsed, err := bundle.ParseMember(raw)
		if err != nil {
			continue
		}
		if !assetFileExists(repoDir, bo.VaultName, parsed.Type, parsed.Name) {
			continue
		}
		items = append(items, AssetSelectionItem{
			Name:  parsed.Name,
			Type:  parsed.Type,
			Vault: bo.VaultName,
		})
	}
	return items
}

// excludeBundleNames 从 names 中剔除 drop 里的名字，保持原顺序。
func excludeBundleNames(names, drop []string) []string {
	if len(names) == 0 || len(drop) == 0 {
		return names
	}
	dropSet := make(map[string]struct{}, len(drop))
	for _, name := range drop {
		dropSet[name] = struct{}{}
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if _, skip := dropSet[name]; skip {
			continue
		}
		out = append(out, name)
	}
	return out
}

// normalizeEnabledBundles 去重、去空白，保持调用方传入的原始顺序。
func normalizeEnabledBundles(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, raw := range names {
		name := trimSpaceASCII(raw)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func trimSpaceASCII(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// EffectiveEnabledGroup 表示一组与 pull 目标集对齐的有效启用资产，按 bundle 分组。
type EffectiveEnabledGroup struct {
	// Label 为 "bundle/<name>"。
	Label string
	Items []AssetSelectionItem
}

// ListEffectiveEnabledAssets 返回与 pull 目标集对齐的有效启用资产，
// 即所有已启用 bundle 展开的成员，按 (type, vault, name) 去重。
func ListEffectiveEnabledAssets(state *AssetSelectionState) []AssetSelectionItem {
	if state == nil {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]AssetSelectionItem, 0)
	for _, group := range ListEffectiveEnabledGroups(state) {
		for _, item := range group.Items {
			key := effectiveAssetKey(item)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}

// ListEffectiveEnabledGroups 按 bundle 名字典序列出已启用 bundle 及其成员。
func ListEffectiveEnabledGroups(state *AssetSelectionState) []EffectiveEnabledGroup {
	if state == nil {
		return nil
	}
	var groups []EffectiveEnabledGroup
	for _, bo := range ListEnabledBundles(state) {
		if len(bo.Members) == 0 {
			continue
		}
		groups = append(groups, EffectiveEnabledGroup{
			Label: "bundle/" + bo.Name,
			Items: append([]AssetSelectionItem(nil), bo.Members...),
		})
	}
	return groups
}

func effectiveAssetKey(item AssetSelectionItem) string {
	return item.Type + ":" + item.Vault + ":" + item.Name
}

// ListEnabledBundles 返回本次 pull 会命中的 bundle 选项，供 TUI Pull 计划 / Remove 等场景展示。
//
// 启用状态已由 Workspace 平面隔离，不再合并另一个平面的选择。
func ListEnabledBundles(state *AssetSelectionState) []AssetBundleOption {
	if state == nil {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]AssetBundleOption, 0)
	for _, bo := range state.Bundles {
		if !bo.Enabled {
			continue
		}
		if _, dup := seen[bo.Name]; dup {
			continue
		}
		seen[bo.Name] = struct{}{}
		out = append(out, bo)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}
