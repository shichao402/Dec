package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

// ProviderAuthorRoot 返回本机受管、且 declares provides 的提供方源仓根。没有则空。
func ProviderAuthorRoot(project string) string {
	project = strings.TrimSpace(project)
	if !types.IsValidProjectName(project) {
		return ""
	}
	states, err := ListManagedProjectStates()
	if err != nil {
		return ""
	}
	for _, state := range states {
		if !state.Exists || !state.Initialized || state.Error != "" {
			continue
		}
		cfg, err := config.NewProjectConfigManager(state.Root).LoadProjectConfig()
		if err != nil || cfg == nil {
			continue
		}
		if strings.TrimSpace(cfg.ProjectName) != project {
			continue
		}
		if len(cfg.Provides) == 0 && strings.TrimSpace(cfg.ProvidesRoot) == "" {
			continue
		}
		return state.Root
	}
	return ""
}

func vaultProjectAccess(project string) string {
	var access string
	_ = withLocalReadRepoDir(func(repoDir string) error {
		loaded, err := pmodel.Load(repoDir, project)
		if err != nil {
			return err
		}
		access = loaded.Manifest.Access
		return nil
	})
	return types.EffectiveProviderAccess(access)
}

type ProviderAccessItem struct {
	Project    string
	Pin        string
	Source     string
	Access     string
	OriginRepo string
	AuthorRoot string
}

type ProviderAccessState struct {
	Items []ProviderAccessItem
}

func ListProviderAccess(ctx context.Context, workspace Workspace, reporter Reporter) (*ProviderAccessState, error) {
	state, err := ListSubscriptionCandidates(ctx, workspace, reporter)
	if err != nil {
		return nil, err
	}
	out := &ProviderAccessState{}
	for _, b := range state.Bundles {
		if b.SecretsOnly || b.OtherPlane {
			continue
		}
		out.Items = append(out.Items, ProviderAccessItem{
			Project:    b.Name,
			Pin:        b.Pin,
			Source:     b.Source,
			Access:     types.EffectiveProviderAccess(b.Access),
			OriginRepo: b.OriginRepo,
			AuthorRoot: firstNonEmpty(b.AuthorRoot, ProviderAuthorRoot(b.Name)),
		})
	}
	return out, nil
}

type SaveProjectAccessInput struct {
	Name   string
	Access string
}

type SaveProjectAccessResult struct {
	Name      string
	Access    string
	Committed bool
}

func SaveProjectAccess(in SaveProjectAccessInput, reporter Reporter) (*SaveProjectAccessResult, error) {
	reporter = defaultReporter(reporter)
	name := strings.TrimSpace(in.Name)
	if !types.IsValidProjectName(name) {
		return nil, fmt.Errorf("项目名 %q 非法", in.Name)
	}
	if err := types.ValidateProviderAccess(in.Access); err != nil {
		return nil, err
	}
	access := types.CanonicalProviderAccess(in.Access)
	result := &SaveProjectAccessResult{Name: name, Access: access}
	err := withAppWriteRepo(func(tx *repo.Transaction) error {
		loaded, loadErr := pmodel.Load(tx.WorkDir(), name)
		if loadErr != nil {
			return fmt.Errorf("加载项目 %q 失败: %w", name, loadErr)
		}
		manifest := loaded.Manifest
		if types.CanonicalProviderAccess(manifest.Access) == access {
			result.Access = manifest.Access
			emit(reporter, EventInfo, "p.access", fmt.Sprintf("项目 %s access 未变化", name), nil)
			return nil
		}
		manifest.Access = access
		if err := pmodel.SaveManifest(tx.WorkDir(), manifest); err != nil {
			return err
		}
		committed, err := tx.CommitAndPush("p: update " + name + " access")
		if err != nil {
			return err
		}
		result.Committed = committed
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.Committed {
		emit(reporter, EventInfo, "p.access", fmt.Sprintf("项目 %s access 已推送到私仓：%s", name, access), nil)
	}
	return result, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
