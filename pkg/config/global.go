package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/editor"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
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
// 兼容旧版本 ~/.dec/local/config.yaml 中的 IDE 配置，并在内存中合并到返回值。
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

	return config, nil
}

// SaveGlobalConfig 保存全局配置，并在成功后清理旧版 ~/.dec/local/config.yaml。
func SaveGlobalConfig(config *types.GlobalConfig) error {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	header := "# Dec 全局配置\n# repo_url: 个人资产仓库地址\n# ides: 默认 IDE 列表，例如：\n#   ides:\n#     - cursor\n#     - codebuddy\n# editor: 交互式编辑器命令（如 vim / vi / code --wait），例如：\n#   editor: code --wait\n\n"
	if err := os.WriteFile(configPath, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("写入全局配置失败: %w", err)
	}

	if err := removeLegacyLocalConfig(); err != nil {
		return err
	}

	return nil
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

// SystemConfig 系统配置
type SystemConfig struct {
	RepoOwner    string
	RepoName     string
	VersionURL   string
	UpdateBranch string
}

// GetSystemConfig 获取系统配置（返回默认值）
func GetSystemConfig() *SystemConfig {
	return &SystemConfig{
		RepoOwner:    "shichao402",
		RepoName:     "Dec",
		VersionURL:   "https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/version.json",
		UpdateBranch: "ReleaseLatest",
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
	return "https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/version.json"
}
