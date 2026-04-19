package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

type AssetSelectionItem struct {
	Name    string
	Type    string
	Vault   string
	Enabled bool
}

type AssetSelectionState struct {
	ProjectRoot    string
	ConfigPath     string
	VarsPath       string
	ExistingConfig bool
	VarsFileReady  bool
	Items          []AssetSelectionItem
}

type SaveAssetSelectionResult struct {
	ConfigPath     string
	VarsPath       string
	VarsCreated    bool
	AvailableCount int
	EnabledCount   int
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
	state.Items = buildAssetSelectionItems(allAssets, existingConfig)

	if _, err := os.Stat(state.VarsPath); err == nil {
		state.VarsFileReady = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查项目变量文件失败: %w", err)
	}

	emit(reporter, EventInfo, "assets.load", fmt.Sprintf("资产选择状态已加载，共 %d 个资产", len(state.Items)), nil)
	return state, nil
}

func SaveAssetSelection(projectRoot string, items []AssetSelectionItem, reporter Reporter) (*SaveAssetSelectionResult, error) {
	reporter = defaultReporter(reporter)
	mgr := config.NewProjectConfigManager(projectRoot)
	result := &SaveAssetSelectionResult{
		ConfigPath: filepath.Join(mgr.GetDecDir(), "config.yaml"),
		VarsPath:   mgr.GetVarsPath(),
	}

	var existingConfig *types.ProjectConfig
	if mgr.Exists() {
		loadedConfig, err := mgr.LoadProjectConfig()
		if err != nil {
			return nil, err
		}
		existingConfig = loadedConfig
	}

	projectConfig := &types.ProjectConfig{}
	if existingConfig != nil {
		projectConfig.IDEs = append([]string(nil), existingConfig.IDEs...)
		projectConfig.Editor = existingConfig.Editor
	}
	projectConfig.Available = &types.AssetList{}
	projectConfig.Enabled = &types.AssetList{}

	for _, item := range items {
		ref := types.AssetRef{Name: item.Name, Vault: item.Vault}
		projectConfig.Available.Add(item.Type, ref)
		result.AvailableCount++
		if item.Enabled {
			projectConfig.Enabled.Add(item.Type, ref)
			result.EnabledCount++
		}
	}

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

func buildAssetSelectionItems(allAssets []AssetInfo, existingConfig *types.ProjectConfig) []AssetSelectionItem {
	enabled := make(map[string]struct{})
	if existingConfig != nil {
		for _, asset := range existingConfig.Enabled.All() {
			enabled[assetSelectionKey(asset.Type, asset.AssetRef)] = struct{}{}
		}
	}

	items := make([]AssetSelectionItem, 0, len(allAssets))
	for _, asset := range allAssets {
		_, isEnabled := enabled[assetSelectionKey(asset.Type, types.AssetRef{Name: asset.Name, Vault: asset.Vault})]
		items = append(items, AssetSelectionItem{
			Name:    asset.Name,
			Type:    asset.Type,
			Vault:   asset.Vault,
			Enabled: isEnabled,
		})
	}
	return items
}

func assetSelectionKey(assetType string, ref types.AssetRef) string {
	return assetType + "\x00" + ref.Vault + "\x00" + ref.Name
}
