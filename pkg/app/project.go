package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

type AssetInfo struct {
	Name  string
	Type  string
	Vault string
}

type ConfigInitPreparation struct {
	ProjectRoot    string
	ConfigPath     string
	VarsPath       string
	ProjectConfig  *types.ProjectConfig
	ExistingConfig bool
	VarsCreated    bool
	AssetCount     int
}

func PrepareProjectConfigInit(projectRoot string, reporter Reporter) (*ConfigInitPreparation, error) {
	reporter = defaultReporter(reporter)
	connected, err := repo.IsConnected()
	if err != nil {
		return nil, fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return nil, fmt.Errorf("仓库未连接\n\n运行 dec config repo <url> 先连接你的仓库")
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	prepared := &ConfigInitPreparation{
		ProjectRoot: projectRoot,
		ConfigPath:  filepath.Join(mgr.GetDecDir(), "config.yaml"),
		VarsPath:    mgr.GetVarsPath(),
	}

	var existingConfig *types.ProjectConfig
	if mgr.Exists() {
		prepared.ExistingConfig = true
		emit(reporter, EventInfo, "project.init", "检测到现有项目配置，准备刷新 available 列表", nil)
		loadedConfig, err := mgr.LoadProjectConfig()
		if err == nil {
			existingConfig = loadedConfig
		} else {
			emit(reporter, EventWarn, "project.init", fmt.Sprintf("读取现有项目配置失败，继续按空配置刷新 available：%v", err), nil)
		}
	}

	allAssets, err := ScanAvailableAssets(reporter)
	if err != nil {
		return nil, err
	}
	prepared.AssetCount = len(allAssets)
	if len(allAssets) == 0 {
		return prepared, nil
	}

	enabled := &types.AssetList{}
	projectEditor := ""
	var projectIDEs []string
	projectName := ""
	if existingConfig != nil {
		if !existingConfig.Enabled.IsEmpty() {
			enabled = existingConfig.Enabled
		}
		projectEditor = existingConfig.Editor
		projectIDEs = existingConfig.IDEs
		projectName = existingConfig.ProjectName
	}
	// 新项目默认写入 cwd basename 作为 project_name，避免用户后续需要手写。
	// 已有配置时保留原值（即使为空）——不自动填充 basename，防止悄悄篡改用户意图。
	if !prepared.ExistingConfig && strings.TrimSpace(projectName) == "" {
		if base := strings.TrimSpace(filepath.Base(projectRoot)); base != "" && base != "." && base != "/" {
			projectName = base
		}
	}

	prepared.ProjectConfig = &types.ProjectConfig{
		ProjectName: projectName,
		IDEs:        projectIDEs,
		Editor:      projectEditor,
		Available:   buildAssetList(allAssets),
		Enabled:     enabled,
	}

	emit(reporter, EventInfo, "project.init", "写入项目配置", &Progress{Phase: "write", Current: 1, Total: 2})
	if err := mgr.SaveProjectConfig(prepared.ProjectConfig); err != nil {
		return nil, fmt.Errorf("写入配置失败: %w", err)
	}

	varsCreated, err := mgr.EnsureVarsConfigTemplate()
	if err != nil {
		return nil, fmt.Errorf("写入变量定义模板失败: %w", err)
	}
	prepared.VarsCreated = varsCreated

	emit(reporter, EventInfo, "project.init", "项目配置准备完成", &Progress{Phase: "write", Current: 2, Total: 2})
	return prepared, nil
}

func ScanAvailableAssets(reporter Reporter) ([]AssetInfo, error) {
	reporter = defaultReporter(reporter)
	emit(reporter, EventInfo, "repo.scan", "开始扫描仓库资产", nil)

	var allAssets []AssetInfo
	if err := withReadRepoDir(func(repoDir string) error {
		folders, err := readFolderEntries(repoDir)
		if err != nil {
			return fmt.Errorf("读取仓库失败: %w", err)
		}
		for _, folder := range folders {
			allAssets = append(allAssets, listFolderAssets(folder.path, folder.name)...)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	sort.Slice(allAssets, func(i, j int) bool {
		if allAssets[i].Vault != allAssets[j].Vault {
			return allAssets[i].Vault < allAssets[j].Vault
		}
		if allAssets[i].Type != allAssets[j].Type {
			return allAssets[i].Type < allAssets[j].Type
		}
		return allAssets[i].Name < allAssets[j].Name
	})

	emit(reporter, EventInfo, "repo.scan", fmt.Sprintf("扫描完成，共 %d 个资产", len(allAssets)), &Progress{Phase: "scan", Current: len(allAssets), Total: len(allAssets)})
	return allAssets, nil
}

type folderEntry struct {
	name string
	path string
}

func readFolderEntries(repoDir string) ([]folderEntry, error) {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil, err
	}

	var folders []folderEntry
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			folders = append(folders, folderEntry{
				name: entry.Name(),
				path: filepath.Join(repoDir, entry.Name()),
			})
		}
	}
	sort.Slice(folders, func(i, j int) bool {
		return folders[i].name < folders[j].name
	})
	return folders, nil
}

func listFolderAssets(folderDir, folderName string) []AssetInfo {
	var assets []AssetInfo
	for _, subDir := range []string{"skills", "rules", "mcp"} {
		dir := filepath.Join(folderDir, subDir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.Name() == ".gitkeep" {
				continue
			}

			name := entry.Name()
			assetType := subDir
			if subDir == "rules" {
				assetType = "rule"
				name = strings.TrimSuffix(name, ".mdc")
			} else if subDir == "mcp" {
				name = strings.TrimSuffix(name, ".json")
			} else {
				assetType = "skill"
			}

			assets = append(assets, AssetInfo{
				Name:  name,
				Type:  assetType,
				Vault: folderName,
			})
		}
	}
	return assets
}

func buildAssetList(allAssets []AssetInfo) *types.AssetList {
	list := &types.AssetList{}
	for _, asset := range allAssets {
		list.Add(asset.Type, types.AssetRef{Name: asset.Name, Vault: asset.Vault})
	}
	return list
}

func withReadRepoDir(fn func(string) error) error {
	globalConfig, err := config.LoadGlobalConfig()
	if err == nil {
		if err := repo.EnsureConnectedRepoMatches(globalConfig.RepoURL); err != nil {
			return err
		}
	}

	tx, err := repo.NewReadTransaction()
	if err != nil {
		return err
	}
	defer tx.Close()

	return fn(tx.WorkDir())
}
