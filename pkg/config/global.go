package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/paths"
	"gopkg.in/yaml.v3"
)

// DefaultPackagesSource 默认包源地址
const DefaultPackagesSource = "https://github.com/shichao402/MyDecPackage"

// DefaultPackagesVersion 默认包版本
const DefaultPackagesVersion = "latest"

// GlobalConfig 全局配置结构
type GlobalConfig struct {
	// PackagesSource 包源地址（GitHub 仓库 URL）
	PackagesSource string `yaml:"packages_source"`
	// PackagesVersion 包版本（latest 或具体版本号如 v1.0.0）
	PackagesVersion string `yaml:"packages_version"`
}

// GetGlobalConfigPath 获取全局配置文件路径
func GetGlobalConfigPath() (string, error) {
	rootDir, err := paths.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "config.yaml"), nil
}

// LoadGlobalConfig 加载全局配置
// 如果配置文件不存在，返回默认配置
func LoadGlobalConfig() (*GlobalConfig, error) {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return nil, fmt.Errorf("获取配置路径失败: %w", err)
	}

	// 如果文件不存在，返回默认配置
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return &GlobalConfig{
			PackagesSource:  DefaultPackagesSource,
			PackagesVersion: DefaultPackagesVersion,
		}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config GlobalConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 填充默认值
	if config.PackagesSource == "" {
		config.PackagesSource = DefaultPackagesSource
	}
	if config.PackagesVersion == "" {
		config.PackagesVersion = DefaultPackagesVersion
	}

	return &config, nil
}

// SaveGlobalConfig 保存全局配置
func SaveGlobalConfig(config *GlobalConfig) error {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return fmt.Errorf("获取配置路径失败: %w", err)
	}

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	// 添加注释头
	header := `# Dec 全局配置
# packages_source: 包源地址（GitHub 仓库 URL）
# packages_version: 包版本（latest 或具体版本号如 v1.0.0）

`
	content := header + string(data)

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// SetPackagesSource 设置包源地址
func SetPackagesSource(source string) error {
	config, err := LoadGlobalConfig()
	if err != nil {
		return err
	}
	config.PackagesSource = source
	return SaveGlobalConfig(config)
}

// SetPackagesVersion 设置包版本
func SetPackagesVersion(version string) error {
	config, err := LoadGlobalConfig()
	if err != nil {
		return err
	}
	config.PackagesVersion = version
	return SaveGlobalConfig(config)
}

// GetPackagesCacheDir 获取当前版本的包缓存目录
func GetPackagesCacheDir() (string, error) {
	config, err := LoadGlobalConfig()
	if err != nil {
		return "", err
	}

	cacheDir, err := paths.GetCacheDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(cacheDir, "packages-"+config.PackagesVersion), nil
}

// GetPackagesRulesDir 获取包缓存中的规则目录
func GetPackagesRulesDir() (string, error) {
	packagesDir, err := GetPackagesCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(packagesDir, "rules"), nil
}

// GetPackagesMCPDir 获取包缓存中的 MCP 目录
func GetPackagesMCPDir() (string, error) {
	packagesDir, err := GetPackagesCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(packagesDir, "mcp"), nil
}

// GetCustomRulesDir 获取用户自定义规则目录
func GetCustomRulesDir() (string, error) {
	rootDir, err := paths.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "custom", "rules"), nil
}

// GetCustomMCPDir 获取用户自定义 MCP 目录
func GetCustomMCPDir() (string, error) {
	rootDir, err := paths.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "custom", "mcp"), nil
}

// GetCustomDir 获取用户自定义目录
func GetCustomDir() (string, error) {
	rootDir, err := paths.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "custom"), nil
}
