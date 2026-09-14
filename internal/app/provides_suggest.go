package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/types"
)

// ProvideCandidate 是从项目里扫出来的可提供资产。它带齐派生所需的全部字段，
// Console 勾选即可写入 provides，不需要人工拼路径。
type ProvideCandidate struct {
	Source      string
	Type        string
	Name        string
	Visibility  types.AssetVisibility
	Plane       types.AssetPlane
	Target      string
	Origin      string
	Declared    bool
	DeclaredKey string
}

type ProvideCandidatesState struct {
	ProjectName  string
	ProvidesRoot string
	AuthorDirs   []string
	Candidates   []ProvideCandidate
}

// SuggestProjectProvides 扫描项目里已经存在的资产目录，返回可直接登记的候选。
// 它只读，不改配置，也不碰 .dec/sync 工作副本。
func SuggestProjectProvides(projectRoot string) (*ProvideCandidatesState, error) {
	state, err := LoadProjectProvides(projectRoot)
	if err != nil {
		return nil, err
	}
	declared := make(map[string]string, len(state.Provides))
	for key, item := range state.Provides {
		declared[strings.ToLower(filepath.ToSlash(item.Source))] = key
	}

	var out []ProvideCandidate
	seen := map[string]bool{}
	add := func(candidate ProvideCandidate) {
		candidate.Source = filepath.ToSlash(candidate.Source)
		if seen[strings.ToLower(candidate.Source)] {
			return
		}
		if !config.IsValidProvideName(candidate.Name) {
			return
		}
		if key, ok := declared[strings.ToLower(candidate.Source)]; ok {
			candidate.Declared, candidate.DeclaredKey = true, key
		}
		if target, err := config.ProjectProvideTarget(state.ProjectName, types.ProjectProvide{
			Source:     candidate.Source,
			Visibility: candidate.Visibility,
			Plane:      candidate.Plane,
			Type:       candidate.Type,
			Name:       candidate.Name,
		}); err == nil {
			candidate.Target = target
		}
		seen[strings.ToLower(candidate.Source)] = true
		out = append(out, candidate)
	}

	// 只扫作者根下的规范目录。`.dec/` 是 Dec 状态，IDE 目录是渲染目标；
	// 两者都不是作者源，不能靠名字前缀猜测其中哪些文件“也许可以提供”。
	for _, dir := range assetScanDirs(projectRoot, state.ProvidesRoot) {
		for _, kind := range bundle.VaultAssetKinds {
			if kind.Type != dir.kindType && dir.kindType != "" {
				continue
			}
			scanAssetKind(projectRoot, dir, kind, add)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Source < out[j].Source
	})
	return &ProvideCandidatesState{
		ProjectName:  state.ProjectName,
		ProvidesRoot: state.ProvidesRoot,
		AuthorDirs:   state.AuthorDirs,
		Candidates:   out,
	}, nil
}

type assetScanDir struct {
	abs      string
	origin   string
	kindType string
}

// assetScanDirs 仅覆盖作者根下的规范目录。providesRoot 为空时就是仓库根。
func assetScanDirs(projectRoot, providesRoot string) []assetScanDir {
	dirs := make([]assetScanDir, 0, len(bundle.VaultAssetKinds))
	for _, kind := range bundle.VaultAssetKinds {
		rel := config.ProvideAuthorDir(providesRoot, kind.Dir)
		dirs = append(dirs, assetScanDir{filepath.Join(projectRoot, filepath.FromSlash(rel)), "仓库 " + rel + "/", kind.Type})
	}
	return dirs
}

func scanAssetKind(projectRoot string, dir assetScanDir, kind bundle.VaultAssetKind, add func(ProvideCandidate)) {
	entries, err := os.ReadDir(dir.abs)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || entry.IsDir() != kind.DirEntries {
			continue
		}
		if kind.Suffix != "" && !strings.HasSuffix(entry.Name(), kind.Suffix) {
			continue
		}
		source, err := filepath.Rel(projectRoot, filepath.Join(dir.abs, entry.Name()))
		if err != nil || strings.HasPrefix(source, "..") {
			continue
		}
		add(ProvideCandidate{
			Source:     source,
			Type:       kind.Type,
			Name:       bundle.AssetEntryName(kind, entry.Name()),
			Visibility: types.AssetVisibilityPublic,
			Plane:      types.AssetPlaneLocal,
			Origin:     dir.origin,
		})
	}
}
