package config

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/types"
)

var provideNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// DefaultProvidesRoot 是新项目的作者目录基准点。旧项目若已有 provides 且未写该字段，
// 仍按仓库根解释，避免升级后改变既有 source 的含义。
const DefaultProvidesRoot = "DecAssets"

// IsValidProvideName 判断资产短名能否用于派生 vault 目标。
func IsValidProvideName(name string) bool {
	return provideNameRE.MatchString(name) && name != "." && name != ".."
}

// NormalizeProvidesRoot 规范化作者根。空值表示仓库根。
//
// 作者根的每一段都不能以 `.` 开头：`.dec/` 是 Dec 自己写的状态，`.cursor/` 之类是
// pull 的渲染目标，`.secrets/` 是密钥落地点。把作者根指进任何一个，都会让「人写的」
// 和「机器写的」重新混在一棵树里，正是 ADR 0026 要消除的歧义。
func NormalizeProvidesRoot(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if filepath.IsAbs(trimmed) || path.IsAbs(trimmed) || filepath.VolumeName(trimmed) != "" {
		return "", fmt.Errorf("provides_root %q 必须是相对 project root 的路径", raw)
	}
	clean := path.Clean(strings.ReplaceAll(trimmed, "\\", "/"))
	if clean == "." {
		return "", nil
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("provides_root %q 不是安全相对路径", raw)
		}
		if strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("provides_root %q 不能指向点目录：.dec 是 Dec 状态，.cursor 等是渲染目标", raw)
		}
	}
	return clean, nil
}

// ResolveProvidesRoot 为没有任何作者声明的新项目补默认值，同时保留旧项目语义。
func ResolveProvidesRoot(raw string, provides map[string]types.ProjectProvide) (string, error) {
	root, err := NormalizeProvidesRoot(raw)
	if err != nil {
		return "", err
	}
	if root == "" && len(provides) == 0 {
		return DefaultProvidesRoot, nil
	}
	return root, nil
}

// ProvideAuthorDir 返回某类资产在项目里的作者目录（相对 project root）。
func ProvideAuthorDir(providesRoot, kindDir string) string {
	if providesRoot == "" {
		return kindDir
	}
	return path.Join(providesRoot, kindDir)
}

// NormalizeProjectProvides 校验并规范化 provides。它保证 source 与派生 target 均一一对应，
// 且任意两个 source 都不存在祖先/后代重叠。providesRoot 是作者目录的基准点，空值表示仓库根。
func NormalizeProjectProvides(projectName, providesRoot string, in map[string]types.ProjectProvide) (map[string]types.ProjectProvide, error) {
	if len(in) == 0 {
		return nil, nil
	}
	root, err := NormalizeProvidesRoot(providesRoot)
	if err != nil {
		return nil, err
	}
	projectName = strings.TrimSpace(projectName)
	if !types.IsValidPName(projectName) {
		return nil, fmt.Errorf("provides 要求有效的 project_name（小写 kebab-case）")
	}

	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]types.ProjectProvide, len(in))
	sourceOwner := map[string]string{}
	targetOwner := map[string]string{}
	for _, key := range keys {
		if strings.TrimSpace(key) != key || key == "" {
			return nil, fmt.Errorf("provides key %q 不能为空或包含首尾空白", key)
		}
		item := in[key]
		item.Source = filepath.ToSlash(strings.TrimSpace(item.Source))
		item.Visibility = types.AssetVisibility(strings.ToLower(strings.TrimSpace(string(item.Visibility))))
		item.Plane = types.AssetPlane(strings.ToLower(strings.TrimSpace(string(item.Plane))))
		item.Type = strings.ToLower(strings.TrimSpace(item.Type))
		item.Name = strings.TrimSpace(item.Name)

		source, err := normalizeProvideSource(item.Source)
		if err != nil {
			return nil, fmt.Errorf("provides.%s.source: %w", key, err)
		}
		item.Source = source
		target, err := ProjectProvideTarget(projectName, item)
		if err != nil {
			return nil, fmt.Errorf("provides.%s: %w", key, err)
		}
		if err := validateProvideAuthorSource(root, item); err != nil {
			return nil, fmt.Errorf("provides.%s.source: %w", key, err)
		}
		if previous, ok := sourceOwner[strings.ToLower(source)]; ok {
			return nil, fmt.Errorf("provides.%s 与 provides.%s 使用同一 source %q", key, previous, source)
		}
		lowerTarget := strings.ToLower(target)
		if previous, ok := targetOwner[lowerTarget]; ok {
			return nil, fmt.Errorf("provides.%s 与 provides.%s 派生到同一 target %q", key, previous, target)
		}
		for previousSource, previousKey := range sourceOwner {
			if pathContains(previousSource, strings.ToLower(source)) || pathContains(strings.ToLower(source), previousSource) {
				return nil, fmt.Errorf("provides.%s source %q 与 provides.%s source %q 重叠",
					key, source, previousKey, previousSource)
			}
		}
		sourceOwner[strings.ToLower(source)] = key
		targetOwner[lowerTarget] = key
		out[key] = item
	}
	return out, nil
}

func normalizeProvideSource(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("不能为空")
	}
	if filepath.IsAbs(raw) || path.IsAbs(raw) || filepath.VolumeName(raw) != "" {
		return "", fmt.Errorf("%q 必须是相对 project root 的路径", raw)
	}
	clean := path.Clean(strings.ReplaceAll(raw, "\\", "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%q 不能指向项目根或包含 ..", raw)
	}
	if clean == ".dec" || strings.HasPrefix(clean, ".dec/") {
		return "", fmt.Errorf("%q 不能位于 Dec 状态目录内", raw)
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("%q 不是安全相对路径", raw)
		}
	}
	return clean, nil
}

// validateProvideAuthorSource 把提供源限制在作者根下的规范目录。
// `.cursor` 等 IDE 目录是 pull 的渲染目标，`.dec` 是状态目录，都不能反向成为作者源。
func validateProvideAuthorSource(providesRoot string, item types.ProjectProvide) error {
	kind, ok := bundle.KindByType(item.Type)
	if !ok {
		return nil // 类型错误由 ProjectProvideTarget 给出。
	}
	want := path.Join(ProvideAuthorDir(providesRoot, kind.Dir), bundle.AssetFileName(kind, item.Name))
	if item.Source != want {
		return fmt.Errorf("%q 必须是作者目录下的 %q", item.Source, want)
	}
	return nil
}

func pathContains(parent, child string) bool {
	return child != parent && strings.HasPrefix(child, parent+"/")
}

// ProjectProvideTarget 返回资产的规范目标。公开资产是 vault 相对路径；
// secret 是 Bitwarden 逻辑地址，绝不对应 Git 文件。
func ProjectProvideTarget(projectName string, item types.ProjectProvide) (string, error) {
	if item.Visibility != types.AssetVisibilityPublic && item.Visibility != types.AssetVisibilityPrivate {
		return "", fmt.Errorf("visibility %q 必须是 public 或 private", item.Visibility)
	}
	plane := item.Plane
	if plane != types.AssetPlaneGlobal && plane != types.AssetPlaneLocal {
		return "", fmt.Errorf("plane %q 必须是 global 或 local", item.Plane)
	}
	if !provideNameRE.MatchString(item.Name) || item.Name == "." || item.Name == ".." {
		return "", fmt.Errorf("name %q 非法", item.Name)
	}
	// secret 不进 provides：`.secrets/<project>` 整树到 Bitwarden 的映射已由
	// SyncTarget 规则唯一决定，再声明一遍就是第二套规则（见 ADR 0026）。
	if item.Type == "secret" {
		return "", fmt.Errorf("secret 不在 provides 中声明：`.secrets/` 整树由 SyncTarget 规则同步")
	}
	kind, ok := bundle.KindByType(item.Type)
	if !ok {
		return "", fmt.Errorf("type %q 必须是 skill、rule、mcp 或 command", item.Type)
	}
	return path.Join(projectName, string(item.Visibility), string(plane), kind.Dir, bundle.AssetFileName(kind, item.Name)), nil
}
