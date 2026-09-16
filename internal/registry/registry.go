package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

const (
	// Branch 是 Dec 仓上官方快照所在的 orphan 分支。
	Branch = "registry"
	// TagPrefix 避免和 Dec 产品 tag v* 撞名。
	TagPrefix = "registry/"
	// DefaultURL 是官方注册表仓库。
	DefaultURL = "https://github.com/shichao402/Dec.git"
	yankFile   = "yanked.yaml"
)

// Tag 返回 registry/<project>/<version>。
func Tag(project, version string) (string, error) {
	project = strings.TrimSpace(project)
	version = strings.TrimSpace(version)
	if !types.IsValidProjectName(project) {
		return "", fmt.Errorf("项目名 %q 非法", project)
	}
	if version == "" || version == types.RequiresLatest || strings.ContainsAny(version, "/\\") {
		return "", fmt.Errorf("版本 %q 非法", version)
	}
	return TagPrefix + project + "/" + version, nil
}

// ParseTag 解析 registry/<project>/<version>。
func ParseTag(tag string) (project, version string, err error) {
	tag = strings.TrimSpace(tag)
	if !strings.HasPrefix(tag, TagPrefix) {
		return "", "", fmt.Errorf("tag %q 不是 %s 前缀", tag, TagPrefix)
	}
	rest := strings.TrimPrefix(tag, TagPrefix)
	project, version, ok := strings.Cut(rest, "/")
	if !ok || project == "" || version == "" || strings.Contains(version, "/") {
		return "", "", fmt.Errorf("tag %q 格式应为 registry/<项目>/<版本>", tag)
	}
	if !types.IsValidProjectName(project) {
		return "", "", fmt.Errorf("项目名 %q 非法", project)
	}
	return project, version, nil
}

// Yanked 是 registry 分支根目录 yanked.yaml。
type Yanked map[string][]string

func LoadYanked(root string) (Yanked, error) {
	data, err := os.ReadFile(filepath.Join(root, yankFile))
	if err != nil {
		if os.IsNotExist(err) {
			return Yanked{}, nil
		}
		return nil, err
	}
	var y Yanked
	if err := yaml.Unmarshal(data, &y); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", yankFile, err)
	}
	if y == nil {
		y = Yanked{}
	}
	return y, nil
}

func WriteYanked(root string, y Yanked) error {
	if y == nil {
		y = Yanked{}
	}
	for name, vers := range y {
		sort.Strings(vers)
		y[name] = unique(vers)
		if len(y[name]) == 0 {
			delete(y, name)
		}
	}
	data, err := yaml.Marshal(y)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, yankFile), data, 0o644)
}

func (y Yanked) Contains(project, version string) bool {
	for _, v := range y[project] {
		if v == version {
			return true
		}
	}
	return false
}

func (y Yanked) Add(project, version string) {
	if y.Contains(project, version) {
		return
	}
	y[project] = append(y[project], version)
}

func (y Yanked) Remove(project, version string) {
	vers := y[project]
	out := vers[:0]
	for _, v := range vers {
		if v != version {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		delete(y, project)
		return
	}
	y[project] = out
}

// LatestVersion 在 versions 里挑最新未 yank 的（按字符串降序，要求 v 前缀 semver 风格）。
func LatestVersion(project string, versions []string, yanked Yanked) (string, error) {
	var ok []string
	for _, v := range versions {
		if yanked.Contains(project, v) {
			continue
		}
		ok = append(ok, v)
	}
	if len(ok) == 0 {
		return "", fmt.Errorf("项目 %s 没有可用的未 yank 版本", project)
	}
	sort.Slice(ok, func(i, j int) bool { return compareVersion(ok[i], ok[j]) > 0 })
	return ok[0], nil
}

func compareVersion(a, b string) int {
	return strings.Compare(a, b)
}

func unique(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

