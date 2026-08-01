// Package bundle 实现 vault 内 bundle 声明的解析与校验。
//
// Bundle 是把一组天然成套使用的资产（skill / command / rule / mcp）作为一个启用单位的机制。
// 本包只负责「读」与「校验」：加载 bundles/<name>/bundle.yaml 并产出结构化数据。
// 不负责接入 pull reconcile、TUI 渲染或 CLI 命令——这些由其它子卡承担。
package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/types"
	"gopkg.in/yaml.v3"
)

// ValidMemberTypes 列出成员引用允许的类型前缀。
//
// 与 vault 中的四种资产目录对齐：
//   - skills   / skill   -> skill
//   - commands / command -> command
//   - rules    / rule    -> rule
//   - mcp      / mcps    -> mcp
var ValidMemberTypes = []string{"skill", "command", "rule", "mcp"}

// bundleNameRegexp 约束 bundle name 为字母数字加 - _。
var bundleNameRegexp = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// Warning 表示 bundle 解析过程中发现的非致命问题。
//
// 成员不存在、孤立文件之类情况不会阻断加载，而是作为 warning 返回，
// 让上层（pull reporter、TUI 提示）决定如何呈现。
type Warning struct {
	// BundlePath 是产生 warning 的 bundle 文件路径（相对于 vault 根或绝对路径）。
	BundlePath string
	// BundleName 是 bundle 名（若可解析）。
	BundleName string
	// Message 是人类可读的告警文字。
	Message string
}

// ParseMember 把 "<type>/<name>" 形式的成员引用解析为 BundleMember。
//
// 合法前缀同时接受单复数（skill/skills、rule/rules、mcp/mcps），
// 归一到 skill / command / rule / mcp。
func ParseMember(ref string) (types.BundleMember, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return types.BundleMember{}, fmt.Errorf("成员引用为空")
	}

	idx := strings.Index(ref, "/")
	if idx <= 0 || idx == len(ref)-1 {
		return types.BundleMember{}, fmt.Errorf("成员引用 %q 格式非法，应为 <type>/<name>", ref)
	}

	rawType := strings.TrimSpace(ref[:idx])
	name := strings.TrimSpace(ref[idx+1:])
	if name == "" {
		return types.BundleMember{}, fmt.Errorf("成员引用 %q 缺少资产名", ref)
	}

	normalized, ok := normalizeMemberType(rawType)
	if !ok {
		return types.BundleMember{}, fmt.Errorf("成员引用 %q 使用了不支持的类型 %q，仅允许 skill / command / rule / mcp", ref, rawType)
	}
	return types.BundleMember{Type: normalized, Name: name}, nil
}

// LoadBundle 从 bundleDir/bundle.yaml 加载单个 bundle 声明。
//
// 返回：
//   - bundle：成功解析的 bundle；若 bundle.yaml 不存在则返回零值
//   - warnings：非致命告警（成员不存在等）
//   - err：文件访问失败、YAML 解析失败、name/成员非法等致命错误
//
// memberExists 用来回调校验成员引用指向的资产是否在 bundle 目录内实际存在；
// 传 nil 表示跳过存在性校验。
func LoadBundle(bundleDir string, memberExists func(m types.BundleMember) bool) (types.Bundle, []Warning, error) {
	path := filepath.Join(bundleDir, types.BundleManifestFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return types.Bundle{}, nil, nil
		}
		return types.Bundle{}, nil, fmt.Errorf("读取 bundle 文件 %s 失败: %w", path, err)
	}

	bundle, parseErr := parseBundleYAML(data, path)
	if parseErr != nil {
		return types.Bundle{}, nil, parseErr
	}

	var warnings []Warning
	if memberExists != nil {
		for _, raw := range bundle.Members {
			member, _ := ParseMember(raw)
			if !memberExists(member) {
				warnings = append(warnings, Warning{
					BundlePath: path,
					BundleName: bundle.Name,
					Message:    fmt.Sprintf("bundle %q 成员 %s/%s 在 bundles/%s 内不存在", bundle.Name, member.Type, member.Name, bundle.Name),
				})
			}
		}
	}

	return bundle, warnings, nil
}

// LoadRepoBundles 扫描 repoDir/bundles/*/bundle.yaml 并解析为 Bundle 列表。
//
// 返回按 name 升序排列的 bundle 列表；bundle 名重复时返回致命错误。
func LoadRepoBundles(repoDir string, memberExists func(bundleName string, m types.BundleMember) bool) ([]types.Bundle, []Warning, error) {
	bundlesDir := filepath.Join(repoDir, types.VaultBundlesDir)
	entries, err := os.ReadDir(bundlesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("读取 bundle 目录失败: %w", err)
	}

	var (
		bundles  []types.Bundle
		warnings []Warning
	)
	seenNames := make(map[string]string)

	bundleNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			bundleNames = append(bundleNames, entry.Name())
		}
	}
	sort.Strings(bundleNames)

	for _, bundleName := range bundleNames {
		bundlePath := filepath.Join(bundlesDir, bundleName)
		exists := func(m types.BundleMember) bool {
			if memberExists == nil {
				return true
			}
			return memberExists(bundleName, m)
		}
		b, bundleWarnings, loadErr := LoadBundle(bundlePath, exists)
		if loadErr != nil {
			return nil, warnings, fmt.Errorf("加载 bundle %q 失败: %w", bundleName, loadErr)
		}
		if b.Name == "" {
			continue
		}
		path := filepath.Join(bundlePath, types.BundleManifestFileName)
		if prev, dup := seenNames[b.Name]; dup {
			return nil, warnings, fmt.Errorf("vault %s 内 bundle 名 %q 在 %s 与 %s 重复", repoDir, b.Name, prev, path)
		}
		seenNames[b.Name] = path
		warnings = append(warnings, bundleWarnings...)
		bundles = append(bundles, b)
	}

	sort.Slice(bundles, func(i, j int) bool {
		return bundles[i].Name < bundles[j].Name
	})
	return bundles, warnings, nil
}

// Validate 解析并校验单个 bundle YAML 文件内容，返回解析后的 Bundle。
//
// source 用于报错时指明来源（通常是文件路径）。
//
// 致命条件：
//   - YAML 无法解析
//   - name 为空或命名非法
//   - members 为空
//   - 某个 member 引用格式非法
//
// 仅做单文件语法 / 命名 / 成员格式校验，不做跨文件重名、成员存在性、vault 级别检查，
// 这些由 LoadRepoBundles 在聚合阶段处理。
func Validate(data []byte, source string) (types.Bundle, error) {
	return parseBundleYAML(data, source)
}

// parseBundleYAML 解析单个 bundle 文件内容并做致命校验。
func parseBundleYAML(data []byte, source string) (types.Bundle, error) {
	var bundle types.Bundle
	if err := yaml.Unmarshal(data, &bundle); err != nil {
		return types.Bundle{}, fmt.Errorf("解析 bundle 文件 %s 失败: %w", source, err)
	}

	bundle.Name = strings.TrimSpace(bundle.Name)
	if bundle.Name == "" {
		return types.Bundle{}, fmt.Errorf("bundle 文件 %s 缺少 name 字段", source)
	}
	if !bundleNameRegexp.MatchString(bundle.Name) {
		return types.Bundle{}, fmt.Errorf("bundle 文件 %s 的 name %q 非法，只允许字母数字 / - / _，且首字符为字母数字", source, bundle.Name)
	}

	if len(bundle.Members) == 0 {
		return types.Bundle{}, fmt.Errorf("bundle 文件 %s 的 members 不能为空", source)
	}
	for i, raw := range bundle.Members {
		trimmed := strings.TrimSpace(raw)
		if _, err := ParseMember(trimmed); err != nil {
			return types.Bundle{}, fmt.Errorf("bundle 文件 %s 的 members[%d]：%w", source, i, err)
		}
		bundle.Members[i] = trimmed
	}
	return bundle, nil
}

func normalizeMemberType(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "skill", "skills":
		return "skill", true
	case "command", "commands":
		return "command", true
	case "rule", "rules":
		return "rule", true
	case "mcp", "mcps":
		return "mcp", true
	default:
		return "", false
	}
}
