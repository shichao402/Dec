package app

import (
	"path/filepath"
	"sort"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/types"
)

// ProjectProvidesState describes provider-owned source files. Official
// publication is performed by provider CI; there is no local sync worktree.
type ProjectProvidesState struct {
	ProjectName  string
	ConfigPath   string
	ProvidesRoot string
	AuthorDirs   []string
	Provides     map[string]types.ProjectProvide
	Targets      map[string]string
	// Products 是多产品仓的产品声明（ADR 0031/0034/0035）。单产品仓为空，
	// Console 沿用 ProvidesRoot + Provides 的单产品编辑视图。
	Products []ProductProvidesState
}

// ProductProvidesState 是单个产品的作者声明。Source 相对该产品的 Root，
// Target 按产品名派生，与发布链路（AuthorProducts）一致。
type ProductProvidesState struct {
	Name         string
	Root         string
	Tags         []string
	SecretsPlane types.AssetPlane
	Provides     map[string]types.ProjectProvide
	Targets      map[string]string
	AuthorDirs   []string
	// IdentityOnly 表示这是身份型产品（ADR 0034）：provides 为空、只发身份、密钥留在 Bitwarden。
	IdentityOnly bool
}

type SaveProjectProvidesInput struct {
	ProjectRoot  string
	ProvidesRoot string
	Provides     map[string]types.ProjectProvide
	// Products 整体替换 products 声明。非空时与单产品字段互斥（rejectMixedAuthor）。
	Products []ProductProvidesInput
}

// ProductProvidesInput 是 Console 提交的单个产品声明。
type ProductProvidesInput struct {
	Name         string
	Root         string
	Tags         []string
	SecretsPlane types.AssetPlane
	Provides     map[string]types.ProjectProvide
	IdentityOnly bool
}

func LoadProjectProvides(projectRoot string) (*ProjectProvidesState, error) {
	mgr := config.NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	state := &ProjectProvidesState{
		ProjectName:  cfg.ProjectName,
		ConfigPath:   filepath.Join(mgr.GetDecDir(), "config.yaml"),
		ProvidesRoot: cfg.ProvidesRoot,
		AuthorDirs:   authorDirs(cfg.ProvidesRoot),
		Provides:     cloneProvides(cfg.Provides),
		Targets:      map[string]string{},
	}
	for key, item := range cfg.Provides {
		target, err := config.ProjectProvideTarget(cfg.ProjectName, item)
		if err != nil {
			return nil, err
		}
		state.Targets[key] = target
	}
	// 多产品仓直接用归一化后的 products 声明；不经过 AuthorProducts——
	// 那是发布链路的视图，会把单产品仓也折成一项，且把 source 折算成相对仓根。
	if len(cfg.Products) > 0 {
		names := make([]string, 0, len(cfg.Products))
		for name := range cfg.Products {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			product := cfg.Products[name]
			item := ProductProvidesState{
				Name:         name,
				Root:         product.Root,
				Tags:         product.Tags,
				SecretsPlane: product.SecretsPlane,
				Provides:     cloneProvides(product.Provides),
				Targets:      map[string]string{},
				AuthorDirs:   authorDirs(product.Root),
				IdentityOnly: len(product.Provides) == 0,
			}
			for key, provide := range product.Provides {
				target, err := config.ProjectProvideTarget(name, provide)
				if err != nil {
					return nil, err
				}
				item.Targets[key] = target
			}
			state.Products = append(state.Products, item)
		}
	}
	return state, nil
}

func SaveProjectProvides(input SaveProjectProvidesInput) (*ProjectProvidesState, error) {
	mgr := config.NewProjectConfigManager(input.ProjectRoot)
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	if len(input.Products) > 0 {
		declared := map[string]types.ProductDecl{}
		for _, product := range input.Products {
			provides := map[string]types.ProjectProvide{}
			for key, provide := range product.Provides {
				// Console 编辑的 Source 已是相对产品 root 的路径；发布链路把它折算成相对仓根。
				provides[key] = provide
			}
			declared[product.Name] = types.ProductDecl{
				Root:         product.Root,
				Tags:         product.Tags,
				SecretsPlane: product.SecretsPlane,
				Provides:     provides,
			}
		}
		cfg.Products = declared
		// 多产品仓不保留单产品作者声明，避免互斥校验拒绝保存。
		cfg.ProjectName = ""
		cfg.ProvidesRoot = ""
		cfg.Provides = nil
	} else {
		cfg.ProvidesRoot = input.ProvidesRoot
		cfg.Provides = cloneProvides(input.Provides)
		cfg.Products = nil
	}
	if err := mgr.SaveProjectConfig(cfg); err != nil {
		return nil, err
	}
	return LoadProjectProvides(input.ProjectRoot)
}

func authorDirs(providesRoot string) []string {
	out := make([]string, 0, len(bundle.VaultAssetKinds))
	for _, kind := range bundle.VaultAssetKinds {
		out = append(out, config.ProvideAuthorDir(providesRoot, kind.Dir))
	}
	return out
}

func cloneProvides(in map[string]types.ProjectProvide) map[string]types.ProjectProvide {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]types.ProjectProvide, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
