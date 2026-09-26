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
	// Product 是候选所属的产品名；单产品仓为空，Console 归入默认分组。
	Product string
}

type ProvideCandidatesState struct {
	ProjectName  string
	ProvidesRoot string
	AuthorDirs   []string
	Candidates   []ProvideCandidate
}

// SuggestProjectProvides 扫描项目里已经存在的资产目录，返回可直接登记的候选。
// 它只读，不改配置，也不碰 .dec/sync 工作副本。
// 多产品仓（ADR 0031）扫描每个产品的作者根；单产品仓沿用 provides_root。
func SuggestProjectProvides(projectRoot string) (*ProvideCandidatesState, error) {
	state, err := LoadProjectProvides(projectRoot)
	if err != nil {
		return nil, err
	}

	declared := map[string]string{}
	// declaredSource 以「产品 + 相对产品 root 的 source」为唯一键，
	// 同名目录在两个产品下互不冲突。
	for _, product := range state.Products {
		prefix := productKey(product.Name)
		for key, item := range product.Provides {
			declared[prefix+strings.ToLower(item.Source)] = key
		}
	}
	for key, item := range state.Provides {
		declared[productKey("")+strings.ToLower(item.Source)] = key
	}

	var out []ProvideCandidate
	seen := map[string]bool{}
	add := func(product string, sourceRoot string, candidate ProvideCandidate) {
		candidate.Source = filepath.ToSlash(candidate.Source)
		if seen[productKey(product)+strings.ToLower(candidate.Source)] {
			return
		}
		if !config.IsValidProvideName(candidate.Name) {
			return
		}
		candidate.Product = product
		if key, ok := declared[productKey(product)+strings.ToLower(candidate.Source)]; ok {
			candidate.Declared, candidate.DeclaredKey = true, key
		}
		// 派生 target 用产品名（多产品）或 project_name（单产品）作订阅锚点。
		projectName := state.ProjectName
		if product != "" {
			projectName = product
		}
		if target, err := config.ProjectProvideTarget(projectName, types.ProjectProvide{
			Source:     candidate.Source,
			Visibility: candidate.Visibility,
			Plane:      candidate.Plane,
			Type:       candidate.Type,
			Name:       candidate.Name,
		}); err == nil {
			candidate.Target = target
		}
		seen[productKey(product)+strings.ToLower(candidate.Source)] = true
		out = append(out, candidate)
	}

	// 只扫作者根下的规范目录。`.dec/` 是 Dec 状态，IDE 目录是渲染目标；
	// 两者都不是作者源，不能靠名字前缀猜测其中哪些文件“也许可以提供”。
	if len(state.Products) > 0 {
		for _, product := range state.Products {
			for _, dir := range assetScanDirs(projectRoot, product.Root) {
				for _, kind := range bundle.VaultAssetKinds {
					if kind.Type != dir.kindType && dir.kindType != "" {
						continue
					}
					scanAssetKind(projectRoot, dir, kind, func(candidate ProvideCandidate) {
						// 扫描结果 source 相对仓根，产品声明的 source 相对产品 root；
						// 折算后再比对与登记。
						if product.Root != "" && strings.HasPrefix(candidate.Source, product.Root+"/") {
							candidate.Source = strings.TrimPrefix(candidate.Source, product.Root+"/")
						}
						add(product.Name, product.Root, candidate)
					})
				}
			}
		}
	} else {
		for _, dir := range assetScanDirs(projectRoot, state.ProvidesRoot) {
			for _, kind := range bundle.VaultAssetKinds {
				if kind.Type != dir.kindType && dir.kindType != "" {
					continue
				}
				scanAssetKind(projectRoot, dir, kind, func(candidate ProvideCandidate) {
					add("", state.ProvidesRoot, candidate)
				})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Product != out[j].Product {
			return out[i].Product < out[j].Product
		}
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

func productKey(product string) string {
	return strings.ToLower(product) + "\x00"
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
