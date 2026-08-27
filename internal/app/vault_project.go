package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

// VaultProjectAutoApplyResult 描述从 vault 应用 project 的结果。
type VaultProjectAutoApplyResult struct {
	ProjectRoot      string
	ConfigPath       string
	VarsPath         string
	ProjectName      string
	EnabledBundles   []string
	Applied          bool
	VarsCreated      bool
	AssetCount       int
	BundleCount      int
	Model            string
	HomeProject      string
	RequiredProjects []string
}

// VaultProjectInference 描述从目录名推断出的 vault project（尚未写入本地配置）。
type VaultProjectInference struct {
	ProjectRoot      string
	ProjectName      string
	VaultPath        string
	EnabledBundles   []string
	IDEs             []string
	Editor           string
	Model            string
	HomeProject      string
	RequiredProjects []string
}

// NeedsVaultProjectAutoApply 判断当前项目是否应尝试从 vault 匹配 project。
// 仅在无 config 或 config 未设置 project_name 时返回 true。
func NeedsVaultProjectAutoApply(projectRoot string) (bool, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return false, fmt.Errorf("判断 vault project 归属需要项目根目录：用户平面没有 project 概念")
	}
	mgr := config.NewProjectConfigManager(projectRoot)
	if !mgr.Exists() {
		return true, nil
	}
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(cfg.ProjectName) == "", nil
}

// LoadVaultProject 从 vault mirror 读取 projects/<name>.yaml。
// 第二个返回值表示文件是否存在。
func LoadVaultProject(repoDir, name string) (*types.Project, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" || repoDir == "" {
		return nil, false, nil
	}
	path := filepath.Join(repoDir, types.VaultProjectPath(name))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("读取 vault project %q 失败: %w", name, err)
	}
	var project types.Project
	if err := yaml.Unmarshal(data, &project); err != nil {
		return nil, true, fmt.Errorf("解析 vault project %q 失败: %w", name, err)
	}
	if strings.TrimSpace(project.Name) == "" {
		project.Name = name
	}
	return &project, true, nil
}

// InferVaultProject 在工作区目录 basename 与 vault projects/<name>.yaml 同名时返回推断结果。
// 不写入任何本地配置；用户已显式设置 project_name 后返回 nil。
func InferVaultProject(projectRoot string, reporter Reporter) (*VaultProjectInference, error) {
	reporter = defaultReporter(reporter)
	if strings.TrimSpace(projectRoot) == "" {
		return nil, fmt.Errorf("vault project 推断需要项目根目录：用户平面没有 project 概念")
	}

	connected, err := repo.IsConnected()
	if err != nil {
		return nil, fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return nil, nil
	}

	needsApply, err := NeedsVaultProjectAutoApply(projectRoot)
	if err != nil {
		return nil, err
	}
	if !needsApply {
		return nil, nil
	}

	projectName, _ := ResolveProjectName(projectRoot, nil)
	if projectName == "" || projectName == "unknown" {
		return nil, nil
	}

	var vaultProject *types.Project
	var inferredP *types.P
	if err := withLocalReadRepoDir(func(repoDir string) error {
		projects, scanErr := pmodel.Scan(repoDir)
		if scanErr != nil {
			return scanErr
		}
		if len(projects) > 0 {
			candidate := strings.ToLower(projectName)
			if !types.IsValidPName(candidate) {
				return nil
			}
			if loaded, ok := projects[candidate]; ok {
				manifest := loaded.Manifest
				inferredP = &manifest
			}
			return nil
		}
		loaded, found, loadErr := LoadVaultProject(repoDir, projectName)
		if loadErr != nil {
			return loadErr
		}
		if !found {
			emit(reporter, EventInfo, "project.infer", fmt.Sprintf("vault 中未找到 projects/%s.yaml，跳过推断", projectName), nil)
			return nil
		}
		vaultProject = loaded
		return nil
	}); err != nil {
		return nil, err
	}
	if vaultProject == nil {
		if inferredP != nil {
			return &VaultProjectInference{
				ProjectRoot: projectRoot, ProjectName: inferredP.Name,
				VaultPath:      types.PManifestPath(inferredP.Name),
				EnabledBundles: append([]string(nil), inferredP.Requires...),
				IDEs:           append([]string(nil), inferredP.IDEs...), Editor: inferredP.Editor,
				Model: "p", HomeProject: inferredP.Name,
				RequiredProjects: append([]string(nil), inferredP.Requires...),
			}, nil
		}
		return nil, nil
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	var existingConfig *types.ProjectConfig
	if mgr.Exists() {
		loaded, loadErr := mgr.LoadProjectConfig()
		if loadErr != nil {
			emit(reporter, EventWarn, "project.infer", fmt.Sprintf("读取现有项目配置失败，继续按 vault project 推断：%v", loadErr), nil)
		} else {
			existingConfig = loaded
		}
	}

	enabledBundles := normalizeEnabledBundles(vaultProject.Bundles)
	if existingConfig != nil && len(existingConfig.EnabledBundles) > 0 {
		enabledBundles = append([]string(nil), existingConfig.EnabledBundles...)
	}

	projectEditor := strings.TrimSpace(vaultProject.Editor)
	projectIDEs := append([]string(nil), vaultProject.IDEs...)
	if existingConfig != nil {
		if projectEditor == "" {
			projectEditor = existingConfig.Editor
		}
		if len(projectIDEs) == 0 {
			projectIDEs = append([]string(nil), existingConfig.IDEs...)
		}
	}

	return &VaultProjectInference{
		ProjectRoot:    projectRoot,
		ProjectName:    projectName,
		VaultPath:      types.VaultProjectPath(projectName),
		EnabledBundles: enabledBundles,
		IDEs:           projectIDEs,
		Editor:         projectEditor,
	}, nil
}

// ApplyVaultProject 将推断的 vault project 写入本地 .dec/config.yaml 与 vars 模板。
// 用户已显式设置 project_name 后不再覆盖。
func ApplyVaultProject(projectRoot string, reporter Reporter) (*VaultProjectAutoApplyResult, error) {
	reporter = defaultReporter(reporter)
	result := &VaultProjectAutoApplyResult{ProjectRoot: projectRoot}

	inference, err := InferVaultProject(projectRoot, reporter)
	if err != nil {
		return nil, err
	}
	if inference == nil {
		return result, nil
	}

	projectName := inference.ProjectName
	result.ProjectName = projectName
	result.Model = inference.Model
	result.HomeProject = inference.HomeProject
	result.RequiredProjects = append([]string(nil), inference.RequiredProjects...)

	mgr := config.NewProjectConfigManager(projectRoot)
	result.ConfigPath = filepath.Join(mgr.GetDecDir(), "config.yaml")
	result.VarsPath = mgr.GetVarsPath()

	var existingConfig *types.ProjectConfig
	if mgr.Exists() {
		loaded, loadErr := mgr.LoadProjectConfig()
		if loadErr != nil {
			emit(reporter, EventWarn, "project.apply", fmt.Sprintf("读取现有项目配置失败，继续按空配置应用 vault project：%v", loadErr), nil)
		} else {
			existingConfig = loaded
		}
	}
	if inference.Model == "p" {
		projectConfig := &types.ProjectConfig{ProjectName: inference.HomeProject}
		if existingConfig != nil {
			projectConfig.IDEs = append([]string(nil), existingConfig.IDEs...)
			projectConfig.Editor = existingConfig.Editor
		}
		if len(projectConfig.IDEs) == 0 {
			projectConfig.IDEs = append([]string(nil), inference.IDEs...)
		}
		if projectConfig.Editor == "" {
			projectConfig.Editor = inference.Editor
		}
		if err := mgr.SaveProjectConfig(projectConfig); err != nil {
			return nil, err
		}
		result.Applied = true
		result.BundleCount = 1 + len(inference.RequiredProjects)
		result.EnabledBundles = append([]string(nil), inference.RequiredProjects...)
		result.VarsCreated, _ = mgr.EnsureVarsConfigTemplate()
		return result, nil
	}

	var vaultProject *types.Project
	if err := withLocalReadRepoDir(func(repoDir string) error {
		loaded, found, loadErr := LoadVaultProject(repoDir, projectName)
		if loadErr != nil {
			return loadErr
		}
		if !found {
			return nil
		}
		vaultProject = loaded
		return nil
	}); err != nil {
		return nil, err
	}
	if vaultProject == nil {
		return result, nil
	}

	allAssets, err := ScanAvailableAssets(reporter)
	if err != nil {
		return nil, err
	}
	result.AssetCount = len(allAssets)

	projectEditor := inference.Editor
	projectIDEs := append([]string(nil), inference.IDEs...)
	enabledBundles := append([]string(nil), inference.EnabledBundles...)
	if existingConfig != nil {
		if projectEditor == "" {
			projectEditor = existingConfig.Editor
		}
		if len(projectIDEs) == 0 {
			projectIDEs = append([]string(nil), existingConfig.IDEs...)
		}
		if len(existingConfig.EnabledBundles) > 0 {
			enabledBundles = append([]string(nil), existingConfig.EnabledBundles...)
		}
	}
	if len(enabledBundles) == 0 {
		enabledBundles = normalizeEnabledBundles(vaultProject.Bundles)
	}
	if projectEditor == "" {
		projectEditor = strings.TrimSpace(vaultProject.Editor)
	}
	if len(projectIDEs) == 0 {
		projectIDEs = append([]string(nil), vaultProject.IDEs...)
	}

	projectConfig := &types.ProjectConfig{
		ProjectName:    projectName,
		IDEs:           projectIDEs,
		Editor:         projectEditor,
		EnabledBundles: enabledBundles,
	}

	if err := withLocalReadRepoDir(func(repoDir string) error {
		_, bundleOverviews, scanErr := scanVaultBundles(repoDir, reporter)
		if scanErr != nil {
			return scanErr
		}
		result.BundleCount = len(bundleOverviews)
		return nil
	}); err != nil {
		emit(reporter, EventWarn, "project.apply", fmt.Sprintf("扫描 bundle 失败，仍会继续写入配置：%v", err), nil)
	}

	emit(reporter, EventInfo, "project.apply", fmt.Sprintf("从 vault 应用 project %q（%d 个 bundle）", projectName, len(enabledBundles)), nil)
	if err := mgr.SaveProjectConfig(projectConfig); err != nil {
		return nil, fmt.Errorf("写入配置失败: %w", err)
	}

	varsCreated, err := mgr.EnsureVarsConfigTemplate()
	if err != nil {
		return nil, fmt.Errorf("写入变量定义模板失败: %w", err)
	}

	result.Applied = true
	result.EnabledBundles = append([]string(nil), enabledBundles...)
	result.VarsCreated = varsCreated
	return result, nil
}

// TryAutoApplyVaultProject 是 ApplyVaultProject 的别名，供测试与显式确认流程调用。
func TryAutoApplyVaultProject(projectRoot string, reporter Reporter) (*VaultProjectAutoApplyResult, error) {
	return ApplyVaultProject(projectRoot, reporter)
}
