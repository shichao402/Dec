// Package vars 提供占位符替换逻辑
// Vault 模板中使用 {{VAR_NAME}} 占位符，pull 时替换为实际值
package vars

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/shichao402/Dec/pkg/types"
	"gopkg.in/yaml.v3"
)

// placeholderRe 匹配 {{VAR_NAME}} 占位符
// 变量名规则：大写字母开头，由大写字母、数字、下划线组成
var placeholderRe = regexp.MustCompile(`\{\{([A-Z][A-Z0-9_]*)\}\}`)

// LoadVarsFile 从指定路径加载变量定义文件
// 文件不存在时返回空配置而非错误
func LoadVarsFile(path string) (*types.VarsConfig, error) {
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

// ResolveVars 按优先级合并变量，返回最终的变量映射
// 优先级（高到低）：assetSpecific > projectVars > globalVars
func ResolveVars(
	globalVars *types.VarsConfig,
	projectVars *types.VarsConfig,
	assetType string,
	assetName string,
	placeholders []string,
) map[string]string {
	result := make(map[string]string)

	for _, key := range placeholders {
		// 1. 项目级按资产限定
		if v, ok := getAssetSpecificVar(projectVars, assetType, assetName, key); ok {
			result[key] = v
			continue
		}
		// 2. 项目级全局
		if projectVars != nil && projectVars.Vars != nil {
			if v, ok := projectVars.Vars[key]; ok {
				result[key] = v
				continue
			}
		}
		// 3. 机器级全局
		if globalVars != nil && globalVars.Vars != nil {
			if v, ok := globalVars.Vars[key]; ok {
				result[key] = v
				continue
			}
		}
		// 未找到：不放入 result，由调用方决定如何处理
	}

	return result
}

// getAssetSpecificVar 从资产特定配置中获取变量
func getAssetSpecificVar(cfg *types.VarsConfig, assetType, assetName, key string) (string, bool) {
	if cfg == nil || cfg.Assets == nil {
		return "", false
	}
	var entries map[string]types.AssetVarEntry
	switch assetType {
	case "mcp":
		entries = cfg.Assets.MCPs
	case "rule":
		entries = cfg.Assets.Rules
	case "skill":
		entries = cfg.Assets.Skills
	}
	if entries == nil {
		return "", false
	}
	entry, ok := entries[assetName]
	if !ok || entry.Vars == nil {
		return "", false
	}
	v, ok := entry.Vars[key]
	return v, ok
}

// ExtractPlaceholders 从文本中提取所有占位符变量名（去重）
func ExtractPlaceholders(content string) []string {
	matches := placeholderRe.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		name := m[1]
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result
}

// HasPlaceholders 检查文本中是否包含占位符
func HasPlaceholders(content string) bool {
	return placeholderRe.MatchString(content)
}

// Substitute 将文本中的占位符替换为实际值
// 返回：替换后的文本、实际使用的变量映射、缺失的变量列表
func Substitute(content string, vars map[string]string) (string, map[string]string, []string) {
	used := make(map[string]string)
	missingSet := make(map[string]bool)

	result := placeholderRe.ReplaceAllStringFunc(content, func(match string) string {
		name := placeholderRe.FindStringSubmatch(match)[1]
		if val, ok := vars[name]; ok {
			used[name] = val
			return val
		}
		missingSet[name] = true
		return match // 保持原样
	})

	var missing []string
	for k := range missingSet {
		missing = append(missing, k)
	}
	sort.Strings(missing)

	return result, used, missing
}

// SubstituteFile 对单个文件执行占位符替换（就地修改）
// 返回实际使用的变量映射和缺失的变量列表
func SubstituteFile(filePath string, vars map[string]string) (map[string]string, []string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}

	content := string(data)
	if !HasPlaceholders(content) {
		return nil, nil, nil
	}

	result, used, missing := Substitute(content, vars)
	if len(used) == 0 {
		return nil, missing, nil
	}

	if err := os.WriteFile(filePath, []byte(result), 0644); err != nil {
		return nil, nil, fmt.Errorf("写入替换结果失败: %w", err)
	}

	return used, missing, nil
}

// SubstituteDir 对目录下所有文件递归执行占位符替换
func SubstituteDir(dirPath string, vars map[string]string) (map[string]string, []string, error) {
	allUsed := make(map[string]string)
	var allMissing []string

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		used, missing, err := SubstituteFile(path, vars)
		if err != nil {
			return err
		}
		for k, v := range used {
			allUsed[k] = v
		}
		allMissing = append(allMissing, missing...)
		return nil
	})

	return allUsed, allMissing, err
}

// ExtractPlaceholdersFromFile 从文件中提取占位符
func ExtractPlaceholdersFromFile(filePath string) []string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	return ExtractPlaceholders(string(data))
}

// ExtractPlaceholdersFromDir 从目录中递归提取占位符
func ExtractPlaceholdersFromDir(dirPath string) []string {
	seen := make(map[string]bool)
	var result []string

	filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		for _, p := range ExtractPlaceholdersFromFile(path) {
			if !seen[p] {
				seen[p] = true
				result = append(result, p)
			}
		}
		return nil
	})

	return result
}
