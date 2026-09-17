package app

import (
	"path/filepath"

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
}

type SaveProjectProvidesInput struct {
	ProjectRoot  string
	ProvidesRoot string
	Provides     map[string]types.ProjectProvide
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
	return state, nil
}

func SaveProjectProvides(input SaveProjectProvidesInput) (*ProjectProvidesState, error) {
	mgr := config.NewProjectConfigManager(input.ProjectRoot)
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	cfg.ProvidesRoot = input.ProvidesRoot
	cfg.Provides = cloneProvides(input.Provides)
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
