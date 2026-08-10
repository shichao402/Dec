package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/shichao402/Dec/pkg/bundle"
	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
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
	// Enabled 表示当前 ProjectConfig.EnabledBundles 是否已引用该 bundle。
	Enabled bool
	// UserEnabled 表示本机 Settings 的 user_enabled_bundles 是否已包含该 bundle。
	// 与 Enabled 独立；pull 取二者并集（见 ADR 0003）。
	UserEnabled bool
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
}

func LoadAssetSelection(projectRoot string, reporter Reporter) (*AssetSelectionState, error) {
	reporter = defaultReporter(reporter)
	connected, err := repo.IsConnected()
	if err != nil {
		return nil, fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return nil, fmt.Errorf("仓库未连接\n\n在 Settings 页填写 Dec 仓库地址后重试")
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	state := &AssetSelectionState{
		ProjectRoot: projectRoot,
		ConfigPath:  filepath.Join(mgr.GetDecDir(), "config.yaml"),
		VarsPath:    mgr.GetVarsPath(),
	}

	var existingConfig *types.ProjectConfig
	if mgr.Exists() {
		state.ExistingConfig = true
		emit(reporter, EventInfo, "assets.load", "检测到现有项目配置，准备加载 bundle 选择状态", nil)
		loadedConfig, err := mgr.LoadProjectConfig()
		if err != nil {
			return nil, err
		}
		existingConfig = loadedConfig
	}

	state.Bundles = loadBundleSelection(existingConfig, reporter)

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
	reporter = defaultReporter(reporter)
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

	projectConfig.EnabledBundles = normalizeEnabledBundles(bundles)
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
	tx, err := repo.NewLocalReadTransaction()
	if err != nil {
		emit(reporter, EventWarn, "assets.bundle",
			fmt.Sprintf("打开仓库只读事务失败，Bundles 页将不展示 bundle: %v", err), nil)
		return nil
	}
	defer tx.Close()

	resolved, err := resolveDesiredAssets(projectConfig, tx.WorkDir(), reporter)
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
	userEnabledSet := make(map[string]struct{})
	if cfg, err := secrets.LoadConfig(); err == nil {
		for _, name := range cfg.UserEnabledBundleNames() {
			userEnabledSet[name] = struct{}{}
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
		if _, ok := userEnabledSet[bo.Name]; ok {
			opt.UserEnabled = true
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

// ListEnabledBundles 返回当前 ProjectConfig.EnabledBundles 引用的 bundle 选项，供 TUI Remove 等场景展示。
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
