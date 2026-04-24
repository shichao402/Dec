package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/shichao402/Dec/pkg/bundle"
	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

type AssetSelectionItem struct {
	Name    string
	Type    string
	Vault   string
	Enabled bool
	// Sources 为该资产的当前来源列表（例如 ["bundle/vikunja"]、["standalone", "bundle/foo"]）。
	// 只在 LoadAssetSelection 结果中填充，保存时会忽略（由 EnabledBundles + Enabled 两个字段决定）。
	Sources []string
}

// AssetBundleOption 描述 Assets 页可勾选的 bundle 节点。
//
// 字段含义与 app.BundleOverview 一致，额外把成员展开成 AssetSelectionItem 级别的定位信息
// （Type + Vault + Name），便于 TUI 渲染成员亮起态。
type AssetBundleOption struct {
	Name        string
	Description string
	Vault       string
	// Members 为 bundle 成员解析后的定位信息，顺序与 bundle YAML 中声明保持一致。
	// 若成员解析失败或资产不存在，这里会跳过（LoadAssetSelection 已通过 reporter 打 warning）。
	Members []AssetSelectionItem
	// Enabled 表示当前 ProjectConfig.EnabledBundles 是否已引用该 bundle。
	Enabled bool
}

type AssetSelectionState struct {
	ProjectRoot    string
	ConfigPath     string
	VarsPath       string
	ExistingConfig bool
	VarsFileReady  bool
	Items          []AssetSelectionItem
	// Bundles 是当前仓库扫描得到的全部 bundle 选项，含未启用的。
	// 仓库未连接或扫描失败时为 nil（调用方应当作"没有 bundle"处理）。
	Bundles []AssetBundleOption
}

// AssetSaveSelection 是保存资产勾选状态时的入参集合。
//
// Items 表示单资产勾选（对应 ProjectConfig.Enabled / Available）。
// EnabledBundles 是 TUI 选中的 bundle 列表（对应 ProjectConfig.EnabledBundles）。
// 两者正交共存：bundle 带入的成员资产不应出现在 Items 里（由调用方保证），
// 避免把"由 bundle 带入"的隐式启用当成独立勾选写入 enabled_assets。
type AssetSaveSelection struct {
	Items          []AssetSelectionItem
	EnabledBundles []string
}

type SaveAssetSelectionResult struct {
	ConfigPath         string
	VarsPath           string
	VarsCreated        bool
	AvailableCount     int
	EnabledCount       int
	EnabledBundleCount int
}

func LoadAssetSelection(projectRoot string, reporter Reporter) (*AssetSelectionState, error) {
	reporter = defaultReporter(reporter)
	connected, err := repo.IsConnected()
	if err != nil {
		return nil, fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return nil, fmt.Errorf("仓库未连接\n\n运行 dec config repo <url> 先连接你的仓库")
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
		emit(reporter, EventInfo, "assets.load", "检测到现有项目配置，准备加载资产选择状态", nil)
		loadedConfig, err := mgr.LoadProjectConfig()
		if err != nil {
			return nil, err
		}
		existingConfig = loadedConfig
	}

	allAssets, err := ScanAvailableAssets(reporter)
	if err != nil {
		return nil, err
	}

	// 扫描仓库内 bundle 声明并解析出当前 bundle 带入的资产来源。
	// 失败不阻塞 Assets 页加载：用户仍能按单资产勾选。
	bundles, assetSources := loadBundleSelection(existingConfig, reporter)
	state.Bundles = bundles

	state.Items = buildAssetSelectionItems(allAssets, existingConfig, assetSources)

	if _, err := os.Stat(state.VarsPath); err == nil {
		state.VarsFileReady = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查项目变量文件失败: %w", err)
	}

	emit(reporter, EventInfo, "assets.load", fmt.Sprintf("资产选择状态已加载，共 %d 个资产，%d 个 bundle", len(state.Items), len(state.Bundles)), nil)
	return state, nil
}

func SaveAssetSelection(projectRoot string, selection AssetSaveSelection, reporter Reporter) (*SaveAssetSelectionResult, error) {
	reporter = defaultReporter(reporter)
	mgr := config.NewProjectConfigManager(projectRoot)
	result := &SaveAssetSelectionResult{
		ConfigPath: filepath.Join(mgr.GetDecDir(), "config.yaml"),
		VarsPath:   mgr.GetVarsPath(),
	}

	// 先加载磁盘上的 ProjectConfig 作为起点，避免把未参与本次交互的字段（例如 Version / 其它元数据）
	// 在保存时清零。TUI Assets 页只负责三组字段：Available / Enabled / EnabledBundles；
	// 其余（IDEs / Editor / Version / 未来新字段）一律从原 config 原样带过。
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	if projectConfig == nil {
		projectConfig = &types.ProjectConfig{}
	}

	projectConfig.Available = &types.AssetList{}
	projectConfig.Enabled = &types.AssetList{}

	for _, item := range selection.Items {
		ref := types.AssetRef{Name: item.Name, Vault: item.Vault}
		projectConfig.Available.Add(item.Type, ref)
		result.AvailableCount++
		if item.Enabled {
			projectConfig.Enabled.Add(item.Type, ref)
			result.EnabledCount++
		}
	}

	// EnabledBundles 语义：
	//   - selection.EnabledBundles == nil  →  不修改（保留磁盘上已有的 EnabledBundles）
	//   - selection.EnabledBundles == []   →  清空（用户明确取消所有 bundle）
	//   - selection.EnabledBundles != nil  →  原样替换
	// 这是 Assets 页保存时不吞 EnabledBundles 的关键：旧接口直接 new 空配置会把 bundle 带入的
	// 隐式启用从磁盘抹掉，导致 pull 后成员资产全部被 cleanup。TUI 层在未进入 Bundle 交互时
	// 应传 nil，在用户动过 bundle 勾选时才传一个（可能为空的）完整列表。
	if selection.EnabledBundles != nil {
		normalizedBundles := normalizeEnabledBundles(selection.EnabledBundles)
		if len(normalizedBundles) == 0 {
			projectConfig.EnabledBundles = nil
		} else {
			projectConfig.EnabledBundles = normalizedBundles
		}
	}
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

	emit(reporter, EventInfo, "assets.save", "资产选择已保存", &Progress{Phase: "write", Current: 2, Total: 2})
	return result, nil
}

// loadBundleSelection 扫描仓库内 bundle 声明并解析出资产来源映射。
//
// 返回：
//   - []AssetBundleOption：所有 bundle 选项（含未启用的）；失败时返回 nil。
//   - map[string][]string：以 assetKey（type:vault:name）为 key 的来源列表，
//     保留"bundle/<name>"格式（与 resolveDesiredAssets 的输出一致）。
//
// 本函数只为 Assets 页展示服务，任何错误都降级为 reporter warning，不向上传播。
func loadBundleSelection(projectConfig *types.ProjectConfig, reporter Reporter) ([]AssetBundleOption, map[string][]string) {
	tx, err := repo.NewReadTransaction()
	if err != nil {
		emit(reporter, EventWarn, "assets.bundle",
			fmt.Sprintf("打开仓库只读事务失败，Assets 页将不展示 bundle: %v", err), nil)
		return nil, nil
	}
	defer tx.Close()

	resolved, err := resolveDesiredAssets(projectConfig, tx.WorkDir(), reporter)
	if err != nil {
		emit(reporter, EventWarn, "assets.bundle",
			fmt.Sprintf("解析 bundle 声明失败，Assets 页将不展示 bundle: %v", err), nil)
		return nil, nil
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

	// 剥离多来源映射：resolved.Sources 的 key 是 assetKey，值里可能含 "standalone"。
	// 外层使用方只关心 bundle 来源，这里过滤掉 standalone 以便 TUI 判断"由 bundle 带入"。
	sources := make(map[string][]string, len(resolved.Sources))
	for key, srcs := range resolved.Sources {
		filtered := make([]string, 0, len(srcs))
		for _, s := range srcs {
			if s == "standalone" {
				continue
			}
			filtered = append(filtered, s)
		}
		if len(filtered) > 0 {
			sources[key] = filtered
		}
	}

	return options, sources
}

func buildAssetSelectionItems(allAssets []AssetInfo, existingConfig *types.ProjectConfig, assetSources map[string][]string) []AssetSelectionItem {
	enabled := make(map[string]struct{})
	if existingConfig != nil {
		for _, asset := range existingConfig.Enabled.All() {
			enabled[assetSelectionKey(asset.Type, asset.AssetRef)] = struct{}{}
		}
	}

	items := make([]AssetSelectionItem, 0, len(allAssets))
	for _, asset := range allAssets {
		key := assetSelectionKey(asset.Type, types.AssetRef{Name: asset.Name, Vault: asset.Vault})
		_, isEnabled := enabled[key]
		items = append(items, AssetSelectionItem{
			Name:    asset.Name,
			Type:    asset.Type,
			Vault:   asset.Vault,
			Enabled: isEnabled,
			Sources: cloneSourceList(assetSources[resolverKey(asset.Type, asset.Vault, asset.Name)]),
		})
	}
	return items
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

// resolverKey 与 bundle_resolver.go 内 assetKey 保持一致：type:vault:name。
// 单独拎出是为了避免在 assets.go 里直接取 types.TypedAssetRef 再传入 assetKey。
func resolverKey(assetType, vault, name string) string {
	return assetType + ":" + vault + ":" + name
}

func cloneSourceList(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
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

func assetSelectionKey(assetType string, ref types.AssetRef) string {
	return assetType + "\x00" + ref.Vault + "\x00" + ref.Name
}
