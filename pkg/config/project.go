package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/types"
	"gopkg.in/yaml.v3"
)

// ========================================
// 项目配置管理 (<project>/.dec/)
// ========================================

// ProjectConfigManager 项目配置管理器
type ProjectConfigManager struct {
	projectRoot string
}

const projectVarsTemplate = `# Dec 项目变量定义
# 资产模板中的 {{VAR_NAME}} 会在 dec pull 时替换
# 优先级（由高到低）：assets.<type>.<name>.vars > vars.yaml > vars.d/*.yaml > 机器级变量 (~/.dec/local/vars.yaml)
#
# vars.d/ 目录：可选，放拆分的变量片段 *.yaml / *.yml，按文件名字典序合并，
#   主文件 vars.yaml 会覆盖 vars.d/ 中的同名键。fragment 里的 assets: 字段会被忽略。

vars:
  # TASK_DOCS_DIR: "docs/tasks"
  # VIKUNJA_PROJECT: "MyProject"
  # 稳定的流程 bucket / type label 由共享资产固化，不需要写进 vars
  # API_BASE_URL: "https://api.example.com"
  # API_TOKEN: "<TOKEN>"

assets:
  skill:
    # my-skill:
    #   vars:
    #     API_TOKEN: "<TOKEN>"
  rule:
    # my-rule:
    #   vars:
    #     API_BASE_URL: "https://api.example.com"
  mcp:
    # my-mcp:
    #   vars:
    #     API_TOKEN: "<TOKEN>"
`

// NewProjectConfigManager 创建项目配置管理器
func NewProjectConfigManager(projectRoot string) *ProjectConfigManager {
	return &ProjectConfigManager{projectRoot: projectRoot}
}

// GetDecDir 获取项目 .dec/ 目录
func (m *ProjectConfigManager) GetDecDir() string {
	return filepath.Join(m.projectRoot, ".dec")
}

// GetVarsPath 获取项目变量定义文件路径
func (m *ProjectConfigManager) GetVarsPath() string {
	return filepath.Join(m.GetDecDir(), "vars.yaml")
}

// GetVarsDir 获取项目变量片段目录路径 (.dec/vars.d)
func (m *ProjectConfigManager) GetVarsDir() string {
	return filepath.Join(m.GetDecDir(), "vars.d")
}

// Exists 检查项目配置是否已存在
func (m *ProjectConfigManager) Exists() bool {
	_, err := os.Stat(filepath.Join(m.GetDecDir(), "config.yaml"))
	return err == nil
}

// ========================================
// 项目配置 (.dec/config.yaml)
// ========================================

type projectConfigVersionProbe struct {
	Version string `yaml:"version"`
}

type legacyProjectConfig struct {
	IDEs      []string         `yaml:"ides,omitempty"`
	Editor    string           `yaml:"editor,omitempty"`
	Available *legacyAssetList `yaml:"available,omitempty"`
	Enabled   *legacyAssetList `yaml:"enabled,omitempty"`
}

type legacyAssetList struct {
	Skills []types.AssetRef `yaml:"skills,omitempty"`
	Rules  []types.AssetRef `yaml:"rules,omitempty"`
	MCPs   []types.AssetRef `yaml:"mcps,omitempty"`
}

// LoadProjectConfig 加载项目配置，自动去重
func (m *ProjectConfigManager) LoadProjectConfig() (*types.ProjectConfig, error) {
	configPath := filepath.Join(m.GetDecDir(), "config.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.ProjectConfig{}, nil
		}
		return nil, fmt.Errorf("读取项目配置失败: %w", err)
	}

	version, err := detectProjectConfigVersion(data)
	if err != nil {
		return nil, fmt.Errorf("解析项目配置失败: %w\n\n请检查 %s 的 YAML 格式是否正确", err, configPath)
	}

	switch version {
	case "", "v1":
		if err := m.migrateProjectConfigV1ToV2(data); err != nil {
			return nil, err
		}
		data, err = os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("读取迁移后的项目配置失败: %w", err)
		}
		return loadProjectConfigV2(data, configPath)
	case types.ProjectConfigVersionV2:
		return loadProjectConfigV2(data, configPath)
	default:
		return nil, fmt.Errorf("不支持的项目配置版本 %q\n\n请升级 Dec 或手动迁移 %s", version, configPath)
	}
}

// SaveProjectConfig 保存项目配置
func (m *ProjectConfigManager) SaveProjectConfig(config *types.ProjectConfig) error {
	decDir := m.GetDecDir()
	if err := os.MkdirAll(decDir, 0755); err != nil {
		return fmt.Errorf("创建 .dec 目录失败: %w", err)
	}

	normalized := *config
	normalized.Version = types.ProjectConfigVersionV2
	if normalized.Available != nil {
		normalized.Available.Dedup()
	}
	if normalized.Enabled != nil {
		normalized.Enabled.Dedup()
	}

	data, err := yaml.Marshal(&normalized)
	if err != nil {
		return fmt.Errorf("序列化项目配置失败: %w", err)
	}

	header := "# Dec 项目配置\n# version: 配置结构版本；当前固定为 v2\n# ides: 项目级 IDE 覆盖（可选），例如：\n#   ides:\n#     - cursor\n#     - codex\n# editor: 项目级交互式编辑器，覆盖全局配置（可选），例如：\n#   editor: code --wait\n#   editor: vim\n# available / enabled: 按 vault -> item -> type 组织，type 使用 skills / rules / mcp\n#   my-vault:\n#     my-asset:\n#       skills: true\n#       rules: true\n\n"
	configPath := filepath.Join(decDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("写入项目配置失败: %w", err)
	}

	return nil
}

// EnsureVarsConfigTemplate 确保项目变量定义模板存在，不覆盖已有文件。
func (m *ProjectConfigManager) EnsureVarsConfigTemplate() (bool, error) {
	decDir := m.GetDecDir()
	if err := os.MkdirAll(decDir, 0755); err != nil {
		return false, fmt.Errorf("创建 .dec 目录失败: %w", err)
	}

	varsPath := m.GetVarsPath()
	if _, err := os.Stat(varsPath); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("检查变量定义文件失败: %w", err)
	}

	if err := os.WriteFile(varsPath, []byte(projectVarsTemplate), 0644); err != nil {
		return false, fmt.Errorf("写入变量定义模板失败: %w", err)
	}

	return true, nil
}

// ========================================
// 项目变量定义 (.dec/vars.yaml)
// ========================================

// LoadVarsConfig 加载项目变量定义
//
// 加载顺序（后者覆盖前者）：
//  1. .dec/vars.d/*.yaml 与 *.yml，按文件名字典序依次合并
//  2. .dec/vars.yaml 主文件（权威最高，覆盖 vars.d/ 的值）
//
// 只合并顶层 `vars:` 字段；fragment 里的 `assets:` 会被忽略。
// 最终返回的 VarsConfig.Assets 仅来自主文件。
//
// 任一 fragment 或主文件解析失败都会整体返回 error。
// 主文件不存在但 vars.d 存在时仍会返回 fragment 合并结果。
// 两者都不存在时返回空 VarsConfig。
func (m *ProjectConfigManager) LoadVarsConfig() (*types.VarsConfig, error) {
	// 1. 加载 vars.d/ 片段，得到合并基线
	merged, err := m.loadVarsDirFragments()
	if err != nil {
		return nil, err
	}

	// 2. 加载主文件 vars.yaml
	varsPath := m.GetVarsPath()
	data, err := os.ReadFile(varsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 主文件缺失：直接返回 fragment 合并结果
			if merged == nil {
				return &types.VarsConfig{}, nil
			}
			return &types.VarsConfig{Vars: merged}, nil
		}
		return nil, fmt.Errorf("读取变量定义失败: %w", err)
	}

	var main types.VarsConfig
	if err := yaml.Unmarshal(data, &main); err != nil {
		return nil, fmt.Errorf("解析变量定义失败: %w", err)
	}

	// 3. 主文件覆盖 fragment
	if merged == nil {
		merged = make(map[string]string, len(main.Vars))
	}
	for k, v := range main.Vars {
		merged[k] = v
	}

	result := &types.VarsConfig{
		Assets: main.Assets,
	}
	if len(merged) > 0 {
		result.Vars = merged
	}
	return result, nil
}

// loadVarsDirFragments 读取 .dec/vars.d/*.yaml{,yml}，按文件名字典序合并顶层 vars:
//
// 返回合并后的 map[string]string（无片段或目录不存在时返回 nil, nil）。
// 任一片段解析失败整体返回 error。
// fragment 里的 assets 字段会被忽略（只取 Vars）。
//
// 文件过滤规则：
//   - 仅 *.yaml / *.yml 扩展名
//   - 跳过目录
//   - 跳过以 `.` 开头的隐藏文件
func (m *ProjectConfigManager) loadVarsDirFragments() (map[string]string, error) {
	dir := m.GetVarsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取变量片段目录失败: %w", err)
	}

	var merged map[string]string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		fragmentPath := filepath.Join(dir, name)
		data, err := os.ReadFile(fragmentPath)
		if err != nil {
			return nil, fmt.Errorf("读取变量片段 %s 失败: %w", fragmentPath, err)
		}

		var fragment types.VarsConfig
		if err := yaml.Unmarshal(data, &fragment); err != nil {
			return nil, fmt.Errorf("解析变量片段 %s 失败: %w", fragmentPath, err)
		}

		if len(fragment.Vars) == 0 {
			continue
		}
		if merged == nil {
			merged = make(map[string]string, len(fragment.Vars))
		}
		for k, v := range fragment.Vars {
			merged[k] = v
		}
	}

	return merged, nil
}

func detectProjectConfigVersion(data []byte) (string, error) {
	var probe projectConfigVersionProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return "", err
	}
	return strings.TrimSpace(probe.Version), nil
}

func loadProjectConfigV2(data []byte, configPath string) (*types.ProjectConfig, error) {
	var config types.ProjectConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析项目配置失败: %w\n\n请检查 %s 的 YAML 格式是否正确", err, configPath)
	}
	config.Version = types.ProjectConfigVersionV2
	if config.Available != nil {
		config.Available.Dedup()
	}
	if config.Enabled != nil {
		config.Enabled.Dedup()
	}
	return &config, nil
}

func (m *ProjectConfigManager) migrateProjectConfigV1ToV2(data []byte) error {
	var legacy legacyProjectConfig
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("解析 v1 项目配置失败: %w", err)
	}

	migrated := &types.ProjectConfig{
		Version:   types.ProjectConfigVersionV2,
		IDEs:      legacy.IDEs,
		Editor:    legacy.Editor,
		Available: legacy.Available.toAssetList(),
		Enabled:   legacy.Enabled.toAssetList(),
	}

	if err := m.SaveProjectConfig(migrated); err != nil {
		return fmt.Errorf("迁移项目配置到 v2 失败: %w", err)
	}
	return nil
}

func (l *legacyAssetList) toAssetList() *types.AssetList {
	if l == nil {
		return nil
	}
	converted := &types.AssetList{
		Skills: append([]types.AssetRef(nil), l.Skills...),
		Rules:  append([]types.AssetRef(nil), l.Rules...),
		MCPs:   append([]types.AssetRef(nil), l.MCPs...),
	}
	converted.Dedup()
	return converted
}
