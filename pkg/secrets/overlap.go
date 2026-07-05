package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateNoOverlap 校验 .dec/ 树与 secrets 落地路径不得相交。
func ValidateNoOverlap(projectRoot string, secretRelativePaths []string) error {
	decDir := filepath.Join(projectRoot, ".dec")
	decPaths, err := listProjectRelativePaths(projectRoot, decDir)
	if err != nil {
		return err
	}

	decSet := make(map[string]struct{}, len(decPaths))
	for _, p := range decPaths {
		decSet[p] = struct{}{}
	}

	var conflicts []string
	seen := make(map[string]struct{}, len(secretRelativePaths))
	for _, raw := range secretRelativePaths {
		rel, err := normalizeProjectRelativePath(raw)
		if err != nil {
			return err
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}

		if strings.HasPrefix(rel, ".dec/") || rel == ".dec" {
			conflicts = append(conflicts, rel)
			continue
		}
		if strings.Contains(rel, "/.dec/") {
			conflicts = append(conflicts, rel)
			continue
		}
		if _, ok := decSet[rel]; ok {
			conflicts = append(conflicts, rel)
		}
	}

	if len(conflicts) == 0 {
		return nil
	}
	return fmt.Errorf("secrets 落地路径与 .dec/ 树冲突: %s", strings.Join(conflicts, ", "))
}

func listProjectRelativePaths(projectRoot, dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	var paths []string
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(projectRoot, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	return paths, err
}

func normalizeProjectRelativePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("secrets 落地路径不能为空")
	}
	clean := filepath.ToSlash(filepath.Clean(trimmed))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("非法 secrets 落地路径: %q", raw)
	}
	if !strings.HasPrefix(clean, ".") {
		clean = "./" + clean
	}
	return clean, nil
}
