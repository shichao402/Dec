package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/types"
)

func NormalizeManagedProjectRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("项目路径不能为空")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("规范化项目路径失败: %w", err)
	}
	return filepath.Clean(abs), nil
}

func ListManagedProjects() ([]types.ManagedProject, error) {
	cfg, err := LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	result := make([]types.ManagedProject, 0, len(cfg.ManagedProjects))
	for _, item := range cfg.ManagedProjects {
		root, normErr := NormalizeManagedProjectRoot(item.Root)
		if normErr != nil {
			continue
		}
		key := strings.ToLower(root)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		item.Root = root
		item.Label = strings.TrimSpace(item.Label)
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Root) < strings.ToLower(result[j].Root)
	})
	return result, nil
}

func RegisterManagedProject(root, label string) (types.ManagedProject, error) {
	normalized, err := NormalizeManagedProjectRoot(root)
	if err != nil {
		return types.ManagedProject{}, err
	}
	cfg, err := LoadGlobalConfig()
	if err != nil {
		return types.ManagedProject{}, err
	}
	item := types.ManagedProject{Root: normalized, Label: strings.TrimSpace(label)}
	for i := range cfg.ManagedProjects {
		existing, normErr := NormalizeManagedProjectRoot(cfg.ManagedProjects[i].Root)
		if normErr == nil && strings.EqualFold(existing, normalized) {
			if item.Label == "" {
				item.Label = cfg.ManagedProjects[i].Label
			}
			cfg.ManagedProjects[i] = item
			return item, SaveGlobalConfig(cfg)
		}
	}
	cfg.ManagedProjects = append(cfg.ManagedProjects, item)
	return item, SaveGlobalConfig(cfg)
}

func RemoveManagedProject(root string) (bool, error) {
	normalized, err := NormalizeManagedProjectRoot(root)
	if err != nil {
		return false, err
	}
	cfg, err := LoadGlobalConfig()
	if err != nil {
		return false, err
	}
	out := cfg.ManagedProjects[:0]
	removed := false
	for _, item := range cfg.ManagedProjects {
		existing, normErr := NormalizeManagedProjectRoot(item.Root)
		if normErr == nil && strings.EqualFold(existing, normalized) {
			removed = true
			continue
		}
		out = append(out, item)
	}
	if !removed {
		return false, nil
	}
	cfg.ManagedProjects = out
	return true, SaveGlobalConfig(cfg)
}
