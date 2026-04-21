package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/assets"
	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

type GlobalSettingsState struct {
	ConfigPath        string
	VarsPath          string
	VarsFileReady     bool
	RepoConnected     bool
	RepoURL           string
	ConnectedRepoURL  string
	AvailableIDEs     []string
	SelectedIDEs      []string
	EffectiveIDEs     []string
	IDEWarnings       []string
	ConfiguredEditor  string
	ConnectedBarePath string
}

type ConnectRepoResult struct {
	RepoURL    string
	ConfigPath string
	BareRepo   string
}

type SaveGlobalSettingsInput struct {
	RepoURL string
	IDEs    []string
}

type SaveGlobalSettingsResult struct {
	RepoURL         string
	IDEs            []string
	ConfigPath      string
	VarsPath        string
	VarsCreated     bool
	BareRepo        string
	InstallWarnings []string
}

func LoadGlobalSettings(reporter Reporter) (*GlobalSettingsState, error) {
	reporter = defaultReporter(reporter)

	configPath, err := config.GetGlobalConfigPath()
	if err != nil {
		return nil, fmt.Errorf("获取全局配置路径失败: %w", err)
	}
	varsPath, err := config.GetGlobalVarsPath()
	if err != nil {
		return nil, fmt.Errorf("获取本机变量路径失败: %w", err)
	}
	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		return nil, err
	}

	state := &GlobalSettingsState{
		ConfigPath:       configPath,
		VarsPath:         varsPath,
		ConfiguredEditor: strings.TrimSpace(globalConfig.Editor),
	}

	availableIDEs := ide.List()
	sort.Strings(availableIDEs)
	state.AvailableIDEs = append(state.AvailableIDEs, availableIDEs...)

	selection, err := config.ResolveEffectiveIDEs(nil)
	if err != nil {
		return nil, err
	}
	state.EffectiveIDEs = append(state.EffectiveIDEs, selection.IDEs...)
	state.IDEWarnings = append(state.IDEWarnings, selection.Warnings...)
	if len(globalConfig.IDEs) > 0 {
		state.SelectedIDEs = append(state.SelectedIDEs, globalConfig.IDEs...)
	} else {
		state.SelectedIDEs = append(state.SelectedIDEs, selection.IDEs...)
	}

	connected, err := repo.IsConnected()
	if err != nil {
		return nil, fmt.Errorf("检查仓库连接失败: %w", err)
	}
	state.RepoConnected = connected
	state.RepoURL = strings.TrimSpace(globalConfig.RepoURL)
	if connected {
		remoteURL, err := repo.GetBareRemoteURL()
		if err != nil {
			return nil, fmt.Errorf("读取当前远端失败: %w", err)
		}
		barePath, err := repo.GetBareRepoDir()
		if err != nil {
			return nil, fmt.Errorf("读取 bare repo 路径失败: %w", err)
		}
		state.ConnectedRepoURL = remoteURL
		state.ConnectedBarePath = barePath
		if strings.TrimSpace(state.RepoURL) == "" {
			state.RepoURL = remoteURL
		}
	}

	if _, err := os.Stat(state.VarsPath); err == nil {
		state.VarsFileReady = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查本机变量模板失败: %w", err)
	}

	emit(reporter, EventInfo, "settings.load", "全局设置已加载", nil)
	return state, nil
}

func ConnectRepo(repoURL string, reporter Reporter) (*ConnectRepoResult, error) {
	reporter = defaultReporter(reporter)
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return nil, fmt.Errorf("仓库地址不能为空")
	}

	emit(reporter, EventInfo, "settings.repo", "开始连接仓库", &Progress{Phase: "connect", Current: 1, Total: 2})
	if err := repo.Connect(repoURL); err != nil {
		return nil, err
	}

	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		return nil, fmt.Errorf("加载全局配置失败: %w", err)
	}
	globalConfig.RepoURL = repoURL
	if err := config.SaveGlobalConfig(globalConfig); err != nil {
		return nil, fmt.Errorf("仓库已连接，但保存全局配置失败: %w", err)
	}

	configPath, err := config.GetGlobalConfigPath()
	if err != nil {
		return nil, fmt.Errorf("获取全局配置路径失败: %w", err)
	}
	bareRepo, err := repo.GetBareRepoDir()
	if err != nil {
		return nil, fmt.Errorf("获取 bare repo 路径失败: %w", err)
	}

	emit(reporter, EventInfo, "settings.repo", "仓库连接完成", &Progress{Phase: "connect", Current: 2, Total: 2})
	return &ConnectRepoResult{
		RepoURL:    repoURL,
		ConfigPath: configPath,
		BareRepo:   bareRepo,
	}, nil
}

func SaveGlobalSettings(input SaveGlobalSettingsInput, reporter Reporter) (*SaveGlobalSettingsResult, error) {
	reporter = defaultReporter(reporter)

	var targetIDEs []string
	var err error
	if input.IDEs == nil {
		targetIDEs = ide.List()
		sort.Strings(targetIDEs)
	} else {
		targetIDEs, err = sanitizeIDESelection(input.IDEs)
		if err != nil {
			return nil, err
		}
		if len(targetIDEs) == 0 {
			return nil, fmt.Errorf("至少选择一个 IDE")
		}
	}

	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		return nil, fmt.Errorf("加载全局配置失败: %w", err)
	}

	targetRepoURL, err := resolveRepoURLForGlobalSettings(strings.TrimSpace(input.RepoURL), globalConfig)
	if err != nil {
		return nil, err
	}

	emit(reporter, EventInfo, "settings.save", "开始保存全局设置", &Progress{Phase: "save", Current: 1, Total: 3})
	if err := repo.Connect(targetRepoURL); err != nil {
		return nil, err
	}

	result := &SaveGlobalSettingsResult{
		RepoURL: targetRepoURL,
		IDEs:    append([]string(nil), targetIDEs...),
	}
	for _, ideName := range targetIDEs {
		if err := InstallBuiltinAssetsForIDE(ideName); err != nil {
			warning := fmt.Sprintf("%s: %s", ideName, err)
			result.InstallWarnings = append(result.InstallWarnings, warning)
			emit(reporter, EventWarn, "settings.install", warning, nil)
			continue
		}
		message := fmt.Sprintf("已为 %s 安装内置资产", ideName)
		emit(reporter, EventInfo, "settings.install", message, nil)
	}

	globalConfig.RepoURL = targetRepoURL
	globalConfig.IDEs = append([]string(nil), targetIDEs...)
	if err := config.SaveGlobalConfig(globalConfig); err != nil {
		return nil, fmt.Errorf("保存全局配置失败: %w", err)
	}

	configPath, err := config.GetGlobalConfigPath()
	if err != nil {
		return nil, fmt.Errorf("获取全局配置路径失败: %w", err)
	}
	varsCreated, err := config.EnsureGlobalVarsTemplate()
	if err != nil {
		return nil, fmt.Errorf("写入本机变量定义模板失败: %w", err)
	}
	varsPath, err := config.GetGlobalVarsPath()
	if err != nil {
		return nil, fmt.Errorf("获取本机变量定义路径失败: %w", err)
	}
	bareRepo, err := repo.GetBareRepoDir()
	if err != nil {
		return nil, fmt.Errorf("获取 bare repo 路径失败: %w", err)
	}

	result.ConfigPath = configPath
	result.VarsPath = varsPath
	result.VarsCreated = varsCreated
	result.BareRepo = bareRepo

	emit(reporter, EventInfo, "settings.save", "已写入全局配置与本机变量模板", &Progress{Phase: "save", Current: 3, Total: 3})
	return result, nil
}

func resolveRepoURLForGlobalSettings(inputRepoURL string, globalConfig *types.GlobalConfig) (string, error) {
	if strings.TrimSpace(inputRepoURL) != "" {
		return strings.TrimSpace(inputRepoURL), nil
	}
	if globalConfig != nil && strings.TrimSpace(globalConfig.RepoURL) != "" {
		return strings.TrimSpace(globalConfig.RepoURL), nil
	}

	connected, err := repo.IsConnected()
	if err != nil {
		return "", fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return "", fmt.Errorf("仓库未连接\n\n运行 dec config repo <url> 先连接你的仓库")
	}

	remoteURL, err := repo.GetBareRemoteURL()
	if err != nil {
		return "", fmt.Errorf("读取当前远端失败: %w", err)
	}
	if strings.TrimSpace(remoteURL) == "" {
		return "", fmt.Errorf("当前仓库远端为空，请先运行 dec config repo <url>")
	}
	return remoteURL, nil
}

func sanitizeIDESelection(ideNames []string) ([]string, error) {
	seen := make(map[string]struct{}, len(ideNames))
	result := make([]string, 0, len(ideNames))
	for _, ideName := range ideNames {
		name := strings.TrimSpace(ideName)
		if name == "" {
			continue
		}
		if err := ValidateIDEName(name); err != nil {
			return nil, err
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result, nil
}

func ValidateIDEName(ideName string) error {
	if ide.IsValid(ideName) {
		return nil
	}
	validIDEs := ide.List()
	sort.Strings(validIDEs)
	return fmt.Errorf("不支持的 IDE: %s (支持: %s)", ideName, strings.Join(validIDEs, ", "))
}

// InstallBuiltinAssetsForIDE 为指定 IDE 安装 Dec 跟随分发的内置资产。
func InstallBuiltinAssetsForIDE(ideName string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户主目录失败: %w", err)
	}

	ideImpl := ide.Get(ideName)
	userRoot := ideImpl.UserRootDir(homeDir)
	bundle := assets.GlobalAssets()

	if err := installBuiltinSkills(filepath.Join(userRoot, "skills"), bundle.Skills); err != nil {
		return fmt.Errorf("安装内置 skills 失败: %w", err)
	}
	if err := installBuiltinRules(filepath.Join(userRoot, "rules"), bundle.Rules); err != nil {
		return fmt.Errorf("安装内置 rules 失败: %w", err)
	}
	if err := installBuiltinMCPs(ideName, userRoot, bundle.MCPs); err != nil {
		return err
	}

	return nil
}

func installBuiltinSkills(skillsDir string, skills []assets.SkillAsset) error {
	if len(skills) == 0 {
		return nil
	}

	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return fmt.Errorf("创建 skills 目录失败: %w", err)
	}

	for _, skill := range skills {
		skillDir := filepath.Join(skillsDir, skill.Name)
		if err := os.RemoveAll(skillDir); err != nil {
			return fmt.Errorf("清理 skill %s 目录失败: %w", skill.Name, err)
		}
		if err := writeBuiltinFiles(skillDir, skill.Files); err != nil {
			return fmt.Errorf("安装 skill %s 失败: %w", skill.Name, err)
		}
	}

	return nil
}

func installBuiltinRules(rulesDir string, rules []assets.RuleAsset) error {
	if len(rules) == 0 {
		return nil
	}

	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return fmt.Errorf("创建 rules 目录失败: %w", err)
	}

	for _, rule := range rules {
		rulePath := filepath.Join(rulesDir, rule.Name+".mdc")
		if err := os.WriteFile(rulePath, rule.Content, 0644); err != nil {
			return fmt.Errorf("写入 rule %s 失败: %w", rule.Name, err)
		}
	}

	return nil
}

func installBuiltinMCPs(ideName, userRoot string, mcps []assets.MCPAsset) error {
	if len(mcps) == 0 {
		return nil
	}

	return fmt.Errorf("IDE %s 暂未实现内置 MCP 分发（用户级根目录: %s）", ideName, userRoot)
}

func writeBuiltinFiles(rootDir string, files []assets.FileAsset) error {
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return err
	}

	for _, file := range files {
		fullPath := filepath.Join(rootDir, filepath.FromSlash(file.RelPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, file.Content, 0644); err != nil {
			return err
		}
	}

	return nil
}
