package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

// AssetMemberTypeSecret 是 Bundles 页展示用的 secrets 成员类型（Note / SSH Key 路径，无正文）。
const AssetMemberTypeSecret = "secret"

// AssetSelectionItem 描述 bundle 内的一个成员资产，仅供展示定位使用。
type AssetSelectionItem struct {
	Name       string
	Type       string
	Vault      string
	Visibility types.AssetVisibility
	Plane      types.AssetPlane
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
	// Model="p" 表示顶层项目；Home/Required 分别表示家项目与直接 requires。
	Model     string
	Home      bool
	Required  bool
	Quadrants map[string]int
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
	// Model 在新仓库为 "p"，用于外部兼容工具区分项目与 legacy bundle。
	Model string
	Plane WorkspacePlane
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
	// 新语义字段；旧字段保留 wire 兼容。
	Model            string
	EnabledProjects  []string
	HomeProject      string
	RequiredProjects []string
	RejectedProjects []string
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
		Plane:       workspace.EffectivePlane(),
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
	for _, option := range state.Bundles {
		if option.Model == "p" {
			state.Model = "p"
			break
		}
	}
	if workspace.EffectivePlane() == WorkspaceUser {
		state.Bundles = appendSecretsOnlyBundleOptions(state.Bundles, workspace, existingConfig, reporter)
	}
	enrichBundleOptionsWithSecretMembers(state.Bundles, reporter)

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
	if usesP, _ := connectedRepositoryUsesPModel(); usesP {
		return saveWorkspacePSelection(workspace, bundles, reporter)
	}
	return saveWorkspaceLegacyBundleSelection(workspace, bundles, reporter)
}

func saveWorkspaceLegacyBundleSelection(workspace Workspace, bundles []string, reporter Reporter) (*SaveBundleSelectionResult, error) {
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

func saveWorkspacePSelection(workspace Workspace, names []string, reporter Reporter) (*SaveBundleSelectionResult, error) {
	reporter = defaultReporter(reporter)
	requested := normalizeEnabledBundles(names)
	result := &SaveBundleSelectionResult{Model: "p"}
	var available map[string]*pmodel.Loaded
	if err := withLocalReadRepoDir(func(repoDir string) error {
		var err error
		available, err = pmodel.Scan(repoDir)
		return err
	}); err != nil {
		return nil, err
	}
	valid := make([]string, 0, len(requested))
	for _, name := range requested {
		if _, ok := available[name]; !ok {
			result.RejectedProjects = append(result.RejectedProjects, name+"（项目不存在）")
			result.RejectedBundles = append(result.RejectedBundles, name+"（项目不存在）")
			continue
		}
		valid = append(valid, name)
	}
	if workspace.EffectivePlane() == WorkspaceUser {
		cfg, err := config.LoadGlobalConfig()
		if err != nil {
			return nil, err
		}
		cfg.EnabledProjects = append([]string(nil), valid...)
		cfg.EnabledBundles = append([]string(nil), valid...)
		if err := config.SaveGlobalConfig(cfg); err != nil {
			return nil, err
		}
		result.ConfigPath, _ = config.GetGlobalConfigPath()
		result.VarsPath, _ = config.GetGlobalVarsPath()
		result.EnabledProjects = append([]string(nil), valid...)
		result.EnabledBundleCount = len(valid)
		emit(reporter, EventInfo, "p.save", fmt.Sprintf("已保存 %d 个用户启用项目", len(valid)), nil)
		return result, nil
	}

	mgr := config.NewProjectConfigManager(workspace.Root)
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	home := strings.TrimSpace(cfg.ProjectName)
	if !types.IsValidPName(home) {
		return nil, fmt.Errorf("项目尚未绑定合法家项目")
	}
	requires := make([]string, 0, len(valid))
	for _, name := range valid {
		if name != home {
			requires = append(requires, name)
		}
	}
	if err := withAppWriteRepo(func(tx *repo.Transaction) error {
		loaded, err := pmodel.Load(tx.WorkDir(), home)
		if err != nil {
			return fmt.Errorf("加载家项目 %q 失败: %w", home, err)
		}
		manifest := loaded.Manifest
		manifest.Requires = requires
		if err := pmodel.SaveManifest(tx.WorkDir(), manifest); err != nil {
			return err
		}
		_, err = tx.CommitAndPush("p: update " + home + " requires")
		return err
	}); err != nil {
		return nil, err
	}
	// 项目模型下本地配置只绑定家项目；requires 的 SSOT 是 <home>/dec.yaml。
	cfg.EnabledBundles = nil
	if err := mgr.SaveProjectConfig(cfg); err != nil {
		return nil, err
	}
	result.ConfigPath = filepath.Join(mgr.GetDecDir(), "config.yaml")
	result.VarsPath = mgr.GetVarsPath()
	result.HomeProject = home
	result.RequiredProjects = append([]string(nil), requires...)
	result.EnabledProjects = append([]string{home}, requires...)
	result.EnabledBundleCount = len(result.EnabledProjects)
	emit(reporter, EventInfo, "p.save", fmt.Sprintf("家项目 %s requires 已保存：%s", home, strings.Join(requires, ", ")), nil)
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
			Model:       bo.Model,
			Home:        bo.Home,
			Required:    bo.Required,
			Quadrants:   bo.Quadrants,
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
	if cfg, err := secrets.LoadConfig(); err == nil {
		known = cfg.KnownSecretBundleNames()
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
		opt := AssetBundleOption{Name: name, Enabled: isEnabled}
		switch {
		case scopes.belongsToOtherPlane(name):
			opt.OtherPlane = true
			opt.Description = "属于项目平面（vault manifest 的 scope 不是 user）"
		case inventory.has(name):
			opt.SecretsOnly = true
			opt.Description = "仓库未登记（Bitwarden 已有，vault 尚无 manifest）"
		case !inventory.Checked:
			// 没问过远端，不下结论。
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

// enrichBundleOptionsWithSecretMembers 把 Bitwarden secrets 条目并入 Bundles 页成员列表。
//
// 有 session 时枚举 folder 下 Note / SSH Key 名（无正文）并覆盖写入本机缓存；
// 无 session / NoopClient / 单 bundle 枚举失败时读缓存回填，不为列成员触发 Console 解锁。
func enrichBundleOptionsWithSecretMembers(options []AssetBundleOption, reporter Reporter) {
	if len(options) == 0 {
		return
	}
	sessionReady := secrets.HasSession() && secrets.HasUserKey()
	client := secretsClientFactory()
	live := sessionReady && client != nil
	if _, isNoop := client.(secrets.NoopClient); isNoop {
		live = false
	}

	updates := make(map[string][]string, len(options))
	for i := range options {
		name := options[i].Name
		var paths []string
		if live {
			listed, listErr := listRemoteSecretMemberPaths(client, name)
			if listErr != nil {
				emit(reporter, EventWarn, "assets.bundle",
					fmt.Sprintf("枚举 bundle %q 的 secrets 成员失败（回退本机缓存）: %v", name, listErr), nil)
				paths = secrets.SecretBundleMembers(name)
			} else {
				updates[name] = listed
				paths = listed
			}
		} else {
			paths = secrets.SecretBundleMembers(name)
		}
		options[i].Members = appendSecretMemberItems(options[i], paths)
	}
	if len(updates) == 0 {
		return
	}
	if err := secrets.RememberAllSecretBundleMembers(updates); err != nil {
		emit(reporter, EventWarn, "assets.bundle",
			fmt.Sprintf("写入 known_secret_bundle_members 失败: %v", err), nil)
	}
}

// listRemoteSecretMemberPaths 枚举一个项目在两个平面上的远端条目名。
// Bundles 页只做成员展示，不区分平面，两侧合并即可。
func listRemoteSecretMemberPaths(client secrets.Client, pName string) ([]string, error) {
	var paths []string
	for _, plane := range []secrets.SyncPlane{secrets.SyncPlaneMachine, secrets.SyncPlaneProject} {
		target, err := secrets.NewPSyncTarget(pName, plane)
		if err != nil {
			return nil, err
		}
		notes, err := client.ListNotes(context.Background(), target)
		if err != nil {
			return nil, err
		}
		keys, err := client.ListSSHKeys(context.Background(), target)
		if err != nil {
			return nil, err
		}
		for _, note := range notes {
			paths = append(paths, note.Name)
		}
		for _, key := range keys {
			paths = append(paths, key.Name)
		}
	}
	return paths, nil
}

func appendSecretMemberItems(opt AssetBundleOption, paths []string) []AssetSelectionItem {
	if len(paths) == 0 {
		return opt.Members
	}
	vault := strings.TrimSpace(opt.Vault)
	if vault == "" {
		vault = opt.Name
	}
	seen := make(map[string]struct{}, len(opt.Members)+len(paths))
	out := make([]AssetSelectionItem, 0, len(opt.Members)+len(paths))
	for _, item := range opt.Members {
		key := item.Type + "\x00" + item.Name
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	for _, raw := range paths {
		path := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
		if path == "" {
			continue
		}
		key := AssetMemberTypeSecret + "\x00" + path
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, AssetSelectionItem{
			Name:  path,
			Type:  AssetMemberTypeSecret,
			Vault: vault,
		})
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
		if bo.Model == "p" {
			parts := strings.SplitN(strings.TrimSpace(raw), "/", 4)
			if len(parts) != 4 {
				continue
			}
			visibility := types.AssetVisibility(parts[0])
			plane := types.AssetPlane(parts[1])
			itemType := bundle.DirToType(parts[2])
			asset := types.TypedAssetRef{
				Type: itemType, Visibility: visibility, Plane: plane,
				AssetRef: types.AssetRef{Name: parts[3], Vault: bo.Name},
			}
			if itemType == "" {
				continue
			}
			if _, err := os.Stat(resolveTypedAssetFile(repoDir, asset)); err != nil {
				continue
			}
			items = append(items, AssetSelectionItem{
				Name: parts[3], Type: itemType, Vault: bo.Name,
				Visibility: visibility, Plane: plane,
			})
			continue
		}
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
		items := effectivePItems(state, bo)
		if len(items) == 0 {
			continue
		}
		groups = append(groups, EffectiveEnabledGroup{
			Label: func() string {
				if bo.Model == "p" {
					if bo.Home {
						return "p/" + bo.Name + " (home)"
					}
					if bo.Required {
						return "p/" + bo.Name + " (requires public/project)"
					}
					return "p/" + bo.Name
				}
				return "bundle/" + bo.Name
			}(),
			Items: items,
		})
	}
	return groups
}

func effectivePItems(state *AssetSelectionState, option AssetBundleOption) []AssetSelectionItem {
	if option.Model != "p" {
		return vaultMemberItems(option.Members)
	}
	out := make([]AssetSelectionItem, 0, len(option.Members))
	for _, item := range option.Members {
		if item.Type == AssetMemberTypeSecret {
			continue
		}
		if state.Plane == WorkspaceUser {
			if item.Plane == types.AssetPlaneUser {
				out = append(out, item)
			}
			continue
		}
		if item.Plane != types.AssetPlaneProject {
			continue
		}
		if option.Required && item.Visibility != types.AssetVisibilityPublic {
			continue
		}
		out = append(out, item)
	}
	return out
}

func vaultMemberItems(members []AssetSelectionItem) []AssetSelectionItem {
	if len(members) == 0 {
		return nil
	}
	out := make([]AssetSelectionItem, 0, len(members))
	for _, item := range members {
		if item.Type == AssetMemberTypeSecret {
			continue
		}
		out = append(out, item)
	}
	return out
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
