// Package bundle 实现 vault 内 bundle 声明的解析与校验。
//
// Bundle 是把一组天然成套使用的资产（skill / rule / mcp）作为一个启用单位的机制。
// 本包只负责「读」与「校验」：加载 vault 根目录下的 bundles/*.yaml 并产出结构化数据。
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

// BundlesDirName 是 vault 内存放 bundle 声明的子目录名。
const BundlesDirName = "bundles"

// ValidMemberTypes 列出成员引用允许的类型前缀。
//
// 与 AssetList 的三种资产类型对齐：
//   - skills / skill -> skill
//   - rules  / rule  -> rule
//   - mcp    / mcps  -> mcp
var ValidMemberTypes = []string{"skill", "rule", "mcp"}

// bundleNameRegexp 约束 bundle name 为字母数字加 - _。
var bundleNameRegexp = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// Warning 表示 bundle 解析过程中发现的非致命问题。
//
// 成员不存在、孤立 YAML 文件之类情况不会阻断加载，而是作为 warning 返回，
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
// 归一到 skill / rule / mcp。
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
		return types.BundleMember{}, fmt.Errorf("成员引用 %q 使用了不支持的类型 %q，仅允许 skill / rule / mcp", ref, rawType)
	}
	return types.BundleMember{Type: normalized, Name: name}, nil
}

// LoadBundles 扫描 vaultPath/bundles/ 下所有 *.yaml（不含子目录）并解析为 Bundle。
//
// 返回：
//   - bundles：成功解析的 bundle 列表，按 name 升序
//   - warnings：非致命告警（成员不存在、非 yaml 文件等）；由于本包不依赖具体的
//     「资产是否存在」实现，成员存在性校验由 MemberExists 回调提供
//   - err：目录访问失败、单个 bundle 致命错误（yaml 解析失败、name/成员非法、重名）
//
// 如果 vaultPath/bundles/ 不存在，返回 (nil, nil, nil)，表示该 vault 没有 bundle。
//
// memberExists 用来回调校验成员引用指向的资产是否在 vault 内实际存在；
// 传 nil 表示跳过存在性校验。
func LoadBundles(vaultPath string, memberExists func(m types.BundleMember) bool) ([]types.Bundle, []Warning, error) {
	dir := filepath.Join(vaultPath, BundlesDirName)
	entries, err := os.ReadDir(dir)
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
	seenNames := make(map[string]string) // name -> file path（用于重名报错）
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isYAMLFile(name) {
			warnings = append(warnings, Warning{
				BundlePath: filepath.Join(dir, name),
				Message:    fmt.Sprintf("bundle 目录下忽略非 yaml 文件 %q", name),
			})
			continue
		}

		path := filepath.Join(dir, name)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, warnings, fmt.Errorf("读取 bundle 文件 %s 失败: %w", path, readErr)
		}

		bundle, parseErr := parseBundleYAML(data, path)
		if parseErr != nil {
			return nil, warnings, parseErr
		}

		if prev, dup := seenNames[bundle.Name]; dup {
			return nil, warnings, fmt.Errorf("vault %s 内 bundle 名 %q 在 %s 与 %s 重复", vaultPath, bundle.Name, prev, path)
		}
		seenNames[bundle.Name] = path

		if memberExists != nil {
			for _, raw := range bundle.Members {
				member, _ := ParseMember(raw) // 此处已在 parseBundleYAML 校验过，忽略错误
				if !memberExists(member) {
					warnings = append(warnings, Warning{
						BundlePath: path,
						BundleName: bundle.Name,
						Message:    fmt.Sprintf("bundle %q 成员 %s/%s 在当前 vault 内不存在", bundle.Name, member.Type, member.Name),
					})
				}
			}
		}

		bundles = append(bundles, bundle)
	}

	sort.Slice(bundles, func(i, j int) bool {
		return bundles[i].Name < bundles[j].Name
	})
	return bundles, warnings, nil
}

// parseBundleYAML 解析单个 bundle 文件内容并做致命校验。
//
// 致命条件：
//   - YAML 无法解析
//   - name 为空或命名非法
//   - members 为空
//   - 某个 member 引用格式非法
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
	// 校验每个成员引用格式合法；归一化 members 文本（去首尾空白），
	// 保留原始顺序以便 TUI / pull 阶段复用。
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
	case "rule", "rules":
		return "rule", true
	case "mcp", "mcps":
		return "mcp", true
	default:
		return "", false
	}
}

func isYAMLFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}
