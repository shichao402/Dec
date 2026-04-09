package cmd

import (
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/ide"
)

// uniqueProjectIDEs 按项目级输出路径去重，避免 internal 包装器重复写入同一目录。
func uniqueProjectIDEs(projectRoot string, ideNames []string) []ide.IDE {
	result := make([]ide.IDE, 0, len(ideNames))
	seen := make(map[string]struct{}, len(ideNames))

	for _, ideName := range ideNames {
		ideImpl := ide.Get(ideName)
		key := projectIDEKey(projectRoot, ideImpl)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, ideImpl)
	}

	return result
}

func projectIDEKey(projectRoot string, ideImpl ide.IDE) string {
	parts := []string{
		filepath.Clean(ideImpl.SkillsDir(projectRoot)),
		filepath.Clean(ideImpl.RulesDir(projectRoot)),
		filepath.Clean(ideImpl.MCPConfigPath(projectRoot)),
	}
	return strings.Join(parts, "|")
}

func projectIDENames(projectIDEs []ide.IDE) []string {
	names := make([]string, 0, len(projectIDEs))
	for _, ideImpl := range projectIDEs {
		names = append(names, ideImpl.Name())
	}
	return names
}
