package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/editor"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

const globalVarsTemplate = `# Dec 本机变量定义
# 资产模板中的 {{VAR_NAME}} 会在 dec pull 时替换
# 这里适合放不希望提交到项目仓库的机器级变量

vars:
  # API_TOKEN: "<TOKEN>"
  # DATABASE_URL: "postgres://user:pass@localhost:5432/db"

assets:
  skill:
    # my-skill:
    #   vars:
    #     API_TOKEN: "<TOKEN>"
  rule:
    # my-rule:
    #   vars:
    #     DATABASE_URL: "postgres://localhost:5432/db"
  mcp:
    # my-mcp:
    #   vars:
    #     API_TOKEN: "<TOKEN>"
`

// ========================================
// 全局配置 (~/.dec/config.yaml)
// ========================================

// GetGlobalConfigPath 获取全局配置文件路径
func GetGlobalConfigPath() (string, error) {
	rootDir, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "config.yaml"), nil
}

// LoadGlobalConfig 加载全局配置。
// 兼容旧版本 ~/.dec/local/config.yaml 中的 IDE 配置，以及旧版
// ~/.dec/secrets/config.yaml 中的 user_enabled_bundles（ADR 0009），并在内存中合并到返回值。
func LoadGlobalConfig() (*types.GlobalConfig, error) {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return nil, err
	}

	config := &types.GlobalConfig{}
	if _, err := os.Stat(configPath); err == nil {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("读取全局配置失败: %w", err)
		}
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("解析全局配置失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取全局配置失败: %w", err)
	}

	legacyIDEs, err := loadLegacyLocalIDEs()
	if err != nil {
		return nil, err
	}
	if len(config.IDEs) == 0 && len(legacyIDEs) > 0 {
		config.IDEs = legacyIDEs
	}

	config.EnabledProjects = NormalizeBundleNames(config.EnabledProjects)
	config.EnabledBundles = NormalizeBundleNames(config.EnabledBundles)
	if len(config.EnabledProjects) > 0 {
		config.EnabledBundles = append([]string(nil), config.EnabledProjects...)
	}
	if len(config.EnabledBundles) == 0 {
		legacyBundles, err := loadLegacySecretsEnabledBundles()
		if err != nil {
			return nil, err
		}
		config.EnabledBundles = legacyBundles
	}

	return config, nil
}

// SaveGlobalConfig 保存全局配置，并在成功后清理旧版 ~/.dec/local/config.yaml
// 与旧版 secrets 配置里的 user_enabled_bundles（已迁到本文件的 enabled_bundles）。
func SaveGlobalConfig(config *types.GlobalConfig) error {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	toWrite := config
	if config != nil {
		normalized := *config
		normalized.EnabledBundles = NormalizeBundleNames(config.EnabledBundles)
		config.EnabledBundles = append([]string(nil), normalized.EnabledBundles...)
		normalized.EnabledProjects = append([]string(nil), normalized.EnabledBundles...)
		normalized.EnabledBundles = nil
		toWrite = &normalized
	}

	data, err := yaml.Marshal(toWrite)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	header := "# Dec 全局配置\n# repo_url: 个人资产仓库地址\n# ides: 默认 IDE 列表，例如：\n#   ides:\n#     - cursor\n# editor: 交互式编辑器命令，例如：\n#   editor: code --wait\n# enabled_projects: 用户平面启用的 P（安装 public/user 与 private/user）\n# P 名必须为小写 kebab-case。\n\n"
	if err := os.WriteFile(configPath, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("写入全局配置失败: %w", err)
	}

	if err := removeLegacyLocalConfig(); err != nil {
		return err
	}
	if err := removeLegacySecretsEnabledBundles(); err != nil {
		return err
	}

	return nil
}

// UserEnabledBundles 返回全局配置中的用户平面启用列表（已规范化）。
func UserEnabledBundles() ([]string, error) {
	config, err := LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	return NormalizeBundleNames(config.EnabledBundles), nil
}

// bundleFolderPrefix 是 Bitwarden folder 里 bundle 级前缀；启用列表只存短名。
const bundleFolderPrefix = "bundle/"

// NormalizeBundleNames 去空白、剥离 bundle/ 前缀、去重，保序。
// 与 secrets.NormalizeBundleNames 同语义（config 不依赖 secrets 包）。
func NormalizeBundleNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimPrefix(strings.TrimSpace(raw), bundleFolderPrefix)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// SetRepoURL 设置仓库 URL
func SetRepoURL(url string) error {
	config, err := LoadGlobalConfig()
	if err != nil {
		return err
	}
	config.RepoURL = url
	return SaveGlobalConfig(config)
}

type EffectiveIDESelection struct {
	IDEs     []string
	Warnings []string
}

// GetEffectiveIDEs 获取有效的 IDE 列表（项目级覆盖全局）
func GetEffectiveIDEs(projectConfig *types.ProjectConfig) ([]string, error) {
	selection, err := ResolveEffectiveIDEs(projectConfig)
	if err != nil {
		return nil, err
	}
	return selection.IDEs, nil
}

var removedBuiltInIDEs = map[string]struct{}{
	"windsurf": {},
	"trae":     {},
}

// ResolveEffectiveIDEs 获取有效 IDE 列表，并返回被忽略的已移除 IDE 警告。
func ResolveEffectiveIDEs(projectConfig *types.ProjectConfig) (*EffectiveIDESelection, error) {
	selection := &EffectiveIDESelection{}

	var projectConfigured configuredIDEs
	if projectConfig != nil && len(projectConfig.IDEs) > 0 {
		projectConfigured = filterConfiguredIDEs(projectConfig.IDEs)
		if len(projectConfigured.IDEs) > 0 {
			if len(projectConfigured.Removed) > 0 {
				selection.Warnings = append(selection.Warnings, formatRemovedIDEWarning("项目配置", projectConfigured.Removed, ""))
			}
			selection.IDEs = projectConfigured.IDEs
			return selection, nil
		}
	}

	globalConfig, err := LoadGlobalConfig()
	if err != nil {
		return nil, err
	}

	var globalConfigured configuredIDEs
	if len(globalConfig.IDEs) > 0 {
		globalConfigured = filterConfiguredIDEs(globalConfig.IDEs)
	}

	if len(projectConfigured.Removed) > 0 && len(projectConfigured.IDEs) == 0 {
		fallbackTarget := "默认 IDE cursor"
		if len(globalConfigured.IDEs) > 0 {
			fallbackTarget = "全局配置"
		}
		selection.Warnings = append(selection.Warnings, formatRemovedIDEWarning("项目配置", projectConfigured.Removed, "将回退到"+fallbackTarget))
	}

	if len(globalConfig.IDEs) > 0 {
		if len(globalConfigured.IDEs) > 0 {
			if len(globalConfigured.Removed) > 0 {
				selection.Warnings = append(selection.Warnings, formatRemovedIDEWarning("全局配置", globalConfigured.Removed, ""))
			}
			selection.IDEs = globalConfigured.IDEs
			return selection, nil
		}
		if len(globalConfigured.Removed) > 0 {
			selection.Warnings = append(selection.Warnings, formatRemovedIDEWarning("全局配置", globalConfigured.Removed, "将回退到默认 IDE cursor"))
		}
	}

	selection.IDEs = []string{"cursor"}
	return selection, nil
}

type configuredIDEs struct {
	IDEs    []string
	Removed []string
}

func filterConfiguredIDEs(ideNames []string) configuredIDEs {
	result := configuredIDEs{IDEs: make([]string, 0, len(ideNames))}
	seenValid := make(map[string]struct{}, len(ideNames))
	seenRemoved := make(map[string]struct{}, len(ideNames))

	for _, ideName := range ideNames {
		name := strings.TrimSpace(ideName)
		if name == "" {
			continue
		}
		if _, removed := removedBuiltInIDEs[name]; removed {
			if _, ok := seenRemoved[name]; ok {
				continue
			}
			seenRemoved[name] = struct{}{}
			result.Removed = append(result.Removed, name)
			continue
		}
		if _, ok := seenValid[name]; ok {
			continue
		}
		seenValid[name] = struct{}{}
		result.IDEs = append(result.IDEs, name)
	}

	return result
}

func formatRemovedIDEWarning(scope string, ideNames []string, suffix string) string {
	message := fmt.Sprintf("%s中的 IDE 已移除内置支持，已忽略: %s", scope, strings.Join(ideNames, ", "))
	if strings.TrimSpace(suffix) == "" {
		return message
	}
	return message + "；" + suffix
}

// GetEffectiveEditor 获取有效的交互编辑器（项目级覆盖全局）。
func GetEffectiveEditor(projectConfig *types.ProjectConfig) (string, error) {
	if projectConfig != nil {
		if editorCmd := strings.TrimSpace(projectConfig.Editor); editorCmd != "" {
			return editorCmd, nil
		}
	}

	globalConfig, err := LoadGlobalConfig()
	if err != nil {
		return "", err
	}
	if editorCmd := strings.TrimSpace(globalConfig.Editor); editorCmd != "" {
		return editorCmd, nil
	}

	return editor.DefaultCommand(), nil
}

func getLegacyLocalConfigPath() (string, error) {
	rootDir, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "local", "config.yaml"), nil
}

func loadLegacyLocalIDEs() ([]string, error) {
	legacyPath, err := getLegacyLocalConfigPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("读取旧本机配置失败: %w", err)
	}

	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return nil, fmt.Errorf("读取旧本机配置失败: %w", err)
	}

	var legacy struct {
		IDEs []string `yaml:"ides,omitempty"`
	}
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("解析旧本机配置失败: %w", err)
	}

	return legacy.IDEs, nil
}

// legacyUserEnabledBundlesKey 是 ADR 0009 之前用户平面启用列表在 secrets 配置里的字段名。
const legacyUserEnabledBundlesKey = "user_enabled_bundles"

func getLegacySecretsConfigPath() (string, error) {
	rootDir, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "secrets", "config.yaml"), nil
}

// loadLegacySecretsEnabledBundles 读取旧版 ~/.dec/secrets/config.yaml 的 user_enabled_bundles。
func loadLegacySecretsEnabledBundles() ([]string, error) {
	legacyPath, err := getLegacySecretsConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取旧 secrets 配置失败: %w", err)
	}

	var legacy struct {
		UserEnabledBundles []string `yaml:"user_enabled_bundles,omitempty"`
	}
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("解析旧 secrets 配置失败: %w", err)
	}

	return NormalizeBundleNames(legacy.UserEnabledBundles), nil
}

// removeLegacySecretsEnabledBundles 删除旧 secrets 配置里的 user_enabled_bundles，保留其余字段与注释。
func removeLegacySecretsEnabledBundles() error {
	legacyPath, err := getLegacySecretsConfigPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取旧 secrets 配置失败: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("解析旧 secrets 配置失败: %w", err)
	}
	if !removeYAMLMappingKey(&doc, legacyUserEnabledBundlesKey) {
		return nil
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("序列化旧 secrets 配置失败: %w", err)
	}
	if err := os.WriteFile(legacyPath, out, 0600); err != nil {
		return fmt.Errorf("清理旧 secrets 启用列表失败: %w", err)
	}
	return nil
}

// removeYAMLMappingKey 从文档根映射中删除指定键，返回是否有改动。
func removeYAMLMappingKey(doc *yaml.Node, key string) bool {
	if doc == nil || len(doc.Content) == 0 {
		return false
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		// 键被删除时把它携带的注释挪到后继键，避免丢失文件头说明。
		if i+2 < len(root.Content) {
			next := root.Content[i+2]
			next.HeadComment = joinYAMLComments(root.Content[i].HeadComment, next.HeadComment)
		}
		root.Content = append(root.Content[:i], root.Content[i+2:]...)
		return true
	}
	return false
}

func joinYAMLComments(first, second string) string {
	switch {
	case strings.TrimSpace(first) == "":
		return second
	case strings.TrimSpace(second) == "":
		return first
	default:
		return first + "\n" + second
	}
}

func removeLegacyLocalConfig() error {
	legacyPath, err := getLegacyLocalConfigPath()
	if err != nil {
		return err
	}
	if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理旧本机配置失败: %w", err)
	}
	return nil
}

// ========================================
// 系统配置（用于版本更新）
// ========================================

const (
	defaultRepoOwner    = "shichao402"
	defaultRepoName     = "Dec"
	defaultUpdateBranch = "ReleaseLatest"
	defaultEntryURL     = "https://updates.firoyang.com/rup/directory/dec.pb"
)

// SystemConfig 系统配置
type SystemConfig struct {
	RepoOwner    string
	RepoName     string
	VersionURL   string
	UpdateBranch string
	// EntryURLs is the RUP directory bootstrap list (client-embedded).
	EntryURLs []string
	Product   string
	Channel   string
}

// GetSystemConfig 获取系统配置（返回默认值）
func GetSystemConfig() *SystemConfig {
	return &SystemConfig{
		RepoOwner: defaultRepoOwner,
		RepoName:  defaultRepoName,
		VersionURL: fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/version.json",
			defaultRepoOwner, defaultRepoName, defaultUpdateBranch),
		UpdateBranch: defaultUpdateBranch,
		EntryURLs:    []string{defaultEntryURL},
		Product:      "dec",
		Channel:      "dev",
	}
}

// ========================================
// 全局变量定义 (~/.dec/local/vars.yaml)
// ========================================

// GetGlobalVarsPath 获取机器级变量定义文件路径
func GetGlobalVarsPath() (string, error) {
	rootDir, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "local", "vars.yaml"), nil
}

// EnsureGlobalVarsTemplate 确保机器级变量定义模板存在，不覆盖已有文件。
func EnsureGlobalVarsTemplate() (bool, error) {
	varsPath, err := GetGlobalVarsPath()
	if err != nil {
		return false, err
	}

	if err := os.MkdirAll(filepath.Dir(varsPath), 0755); err != nil {
		return false, fmt.Errorf("创建变量定义目录失败: %w", err)
	}

	if _, err := os.Stat(varsPath); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("检查变量定义文件失败: %w", err)
	}

	if err := os.WriteFile(varsPath, []byte(globalVarsTemplate), 0644); err != nil {
		return false, fmt.Errorf("写入变量定义模板失败: %w", err)
	}

	return true, nil
}

// LoadGlobalVars 加载机器级全局变量定义
func LoadGlobalVars() (*types.VarsConfig, error) {
	varsPath, err := GetGlobalVarsPath()
	if err != nil {
		return &types.VarsConfig{}, nil
	}
	return loadVarsFile(varsPath)
}

func loadVarsFile(path string) (*types.VarsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.VarsConfig{}, nil
		}
		return nil, fmt.Errorf("读取变量定义失败: %w", err)
	}
	var cfg types.VarsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析变量定义失败: %w", err)
	}
	return &cfg, nil
}

// GetVersionURL 获取版本检查 URL
func GetVersionURL() string {
	return GetSystemConfig().VersionURL
}
