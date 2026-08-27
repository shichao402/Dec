// Package pmodel loads the top-level P and four-quadrant asset model.
package pmodel

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

// Loaded is a validated P manifest plus the assets physically present in Git.
type Loaded struct {
	Manifest types.P
	Assets   []types.TypedAssetRef
}

// Load reads <repo>/<name>/dec.yaml and enumerates all four quadrants.
func Load(repoDir, name string) (*Loaded, error) {
	name = strings.TrimSpace(name)
	if !types.IsValidPName(name) {
		return nil, fmt.Errorf("P 名 %q 非法，必须为小写 kebab-case", name)
	}
	manifestPath := filepath.Join(repoDir, types.PManifestPath(name))
	if info, err := os.Lstat(manifestPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("P 声明 %s 不能是符号链接", manifestPath)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("读取 P 声明 %s 失败: %w", manifestPath, err)
	}
	var manifest types.P
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("解析 P 声明 %s 失败: %w", manifestPath, err)
	}
	manifest.Name = strings.TrimSpace(manifest.Name)
	if manifest.Name != name {
		return nil, fmt.Errorf("P 声明 %s 的 name %q 必须与目录名 %q 一致", manifestPath, manifest.Name, name)
	}
	requires, err := normalizeRequires(manifest.Requires, name)
	if err != nil {
		return nil, fmt.Errorf("P 声明 %s: %w", manifestPath, err)
	}
	manifest.Requires = requires

	loaded := &Loaded{Manifest: manifest}
	for _, visibility := range []types.AssetVisibility{types.AssetVisibilityPublic, types.AssetVisibilityPrivate} {
		for _, plane := range []types.AssetPlane{types.AssetPlaneUser, types.AssetPlaneProject} {
			assets, err := scanQuadrant(repoDir, name, visibility, plane)
			if err != nil {
				return nil, err
			}
			loaded.Assets = append(loaded.Assets, assets...)
		}
	}
	return loaded, nil
}

// SaveManifest validates and writes the only mutable declaration of a P.
// Callers must provide a repository write transaction; this package never
// commits or pushes by itself.
func SaveManifest(repoDir string, manifest types.P) error {
	manifest.Name = strings.TrimSpace(manifest.Name)
	if !types.IsValidPName(manifest.Name) {
		return fmt.Errorf("P 名 %q 非法，必须为小写 kebab-case", manifest.Name)
	}
	requires, err := normalizeRequires(manifest.Requires, manifest.Name)
	if err != nil {
		return err
	}
	manifest.Requires = requires
	data, err := yaml.Marshal(&manifest)
	if err != nil {
		return fmt.Errorf("序列化 P %q 声明失败: %w", manifest.Name, err)
	}
	manifestPath := filepath.Join(repoDir, types.PManifestPath(manifest.Name))
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return fmt.Errorf("创建 P %q 目录失败: %w", manifest.Name, err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return fmt.Errorf("写入 P 声明 %s 失败: %w", manifestPath, err)
	}
	for _, visibility := range []types.AssetVisibility{types.AssetVisibilityPublic, types.AssetVisibilityPrivate} {
		for _, plane := range []types.AssetPlane{types.AssetPlaneUser, types.AssetPlaneProject} {
			if err := os.MkdirAll(filepath.Join(repoDir, types.PQuadrantDir(manifest.Name, visibility, plane)), 0o755); err != nil {
				return fmt.Errorf("创建 P %q 象限 %s/%s 失败: %w", manifest.Name, visibility, plane, err)
			}
		}
	}
	return nil
}

// Scan loads every valid top-level P. Reserved/hidden directories are ignored;
// a directory containing dec.yaml but using an invalid name is a hard error.
func Scan(repoDir string) (map[string]*Loaded, error) {
	if strings.TrimSpace(repoDir) == "" {
		return map[string]*Loaded{}, nil
	}
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil, fmt.Errorf("读取 P 仓库失败: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Name() == types.VaultProjectsDir || entry.Name() == types.VaultBundlesDir {
			continue
		}
		if _, err := os.Lstat(filepath.Join(repoDir, entry.Name(), types.ProjectManifestFileName)); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("顶层目录 %q 缺少 dec.yaml：P 仓库不允许其它顶层目录", entry.Name())
			}
			return nil, err
		}
		if !types.IsValidPName(entry.Name()) {
			return nil, fmt.Errorf("包含 dec.yaml 的顶层目录 %q 非法：P 名必须为小写 kebab-case", entry.Name())
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	out := make(map[string]*Loaded, len(names))
	for _, name := range names {
		p, err := Load(repoDir, name)
		if err != nil {
			return nil, err
		}
		out[name] = p
	}
	return out, nil
}

func normalizeRequires(values []string, self string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		name := strings.TrimSpace(raw)
		if !types.IsValidPName(name) {
			return nil, fmt.Errorf("requires 中的 P 名 %q 非法，必须为小写 kebab-case", raw)
		}
		if name == self {
			return nil, fmt.Errorf("requires 不能直接引用自身 %q", self)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

func scanQuadrant(repoDir, pName string, visibility types.AssetVisibility, plane types.AssetPlane) ([]types.TypedAssetRef, error) {
	root := filepath.Join(repoDir, types.PQuadrantDir(pName, visibility, plane))
	var out []types.TypedAssetRef
	for _, kind := range bundle.VaultAssetKinds {
		dir := filepath.Join(root, kind.Dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("读取 P %q 象限 %s/%s/%s 失败: %w", pName, visibility, plane, kind.Dir, err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("P %q 象限 %s/%s/%s 中不允许符号链接 %q",
					pName, visibility, plane, kind.Dir, entry.Name())
			}
			if kind.DirEntries != entry.IsDir() {
				continue
			}
			if !kind.DirEntries && !strings.HasSuffix(entry.Name(), kind.Suffix) {
				continue
			}
			if entry.IsDir() {
				assetRoot := filepath.Join(dir, entry.Name())
				if err := filepath.WalkDir(assetRoot, func(path string, child os.DirEntry, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if child.Type()&os.ModeSymlink != 0 {
						return fmt.Errorf("P %q 资产 %s 中不允许符号链接 %s", pName, entry.Name(), path)
					}
					return nil
				}); err != nil {
					return nil, err
				}
			}
			name := bundle.AssetEntryName(kind, entry.Name())
			out = append(out, types.TypedAssetRef{
				Type:       kind.Type,
				Visibility: visibility,
				Plane:      plane,
				AssetRef:   types.AssetRef{Name: name, Vault: pName},
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
