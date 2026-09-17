package registry

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/types"
)

// Resolve 把 requires 的值变成 registry tag。versions 是该项目已有的提供方版本列表。
func Resolve(project, want string, versions []string, yanked Yanked) (tag string, version string, err error) {
	project = strings.TrimSpace(project)
	want = strings.TrimSpace(want)
	if want == "" {
		want = types.RequiresLatest
	}
	if want == types.RequiresLatest {
		version, err = LatestVersion(project, versions, yanked)
		if err != nil {
			return "", "", err
		}
	} else {
		version = want
		found := false
		for _, v := range versions {
			if v == version {
				found = true
				break
			}
		}
		if !found {
			return "", "", fmt.Errorf("项目 %s 没有版本 %s", project, version)
		}
	}
	tag, err = Tag(project, version)
	if err != nil {
		return "", "", err
	}
	return tag, version, nil
}

// ProjectsFromTags 从 git tag 列表抽出所有已发布项目名（按名字排序、去重）。
func ProjectsFromTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if !strings.HasPrefix(t, TagPrefix) {
			continue
		}
		rest := strings.TrimPrefix(t, TagPrefix)
		idx := strings.Index(rest, "/")
		if idx <= 0 {
			continue
		}
		project := rest[:idx]
		if !types.IsValidProjectName(project) {
			continue
		}
		if _, ok := seen[project]; ok {
			continue
		}
		seen[project] = struct{}{}
		out = append(out, project)
	}
	sort.Strings(out)
	return out
}

// VersionsFromTags 从 git tag 列表抽出某项目的版本。
func VersionsFromTags(project string, tags []string) []string {
	prefix := TagPrefix + project + "/"
	var out []string
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, prefix) {
			out = append(out, strings.TrimPrefix(t, prefix))
		}
	}
	return out
}
