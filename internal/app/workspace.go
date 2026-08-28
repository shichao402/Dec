package app

import (
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/ide"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

// WorkspacePlane 表示 Dec 当前操作的工作空间平面。
type WorkspacePlane string

const (
	WorkspaceLocal  WorkspacePlane = "local"
	WorkspaceGlobal WorkspacePlane = "global"
	WorkspaceProject               = WorkspaceLocal
	WorkspaceUser                  = WorkspaceGlobal
)

// Workspace 将平面与其可选项目根绑定。用户平面不把全局配置伪装成项目配置。
type Workspace struct {
	Plane WorkspacePlane
	Root  string
}

func NewWorkspace(plane WorkspacePlane, root string) Workspace {
	if plane != WorkspaceGlobal && plane != WorkspaceUser {
		plane = WorkspaceLocal
	} else {
		plane = WorkspaceGlobal
	}
	return Workspace{Plane: plane, Root: strings.TrimSpace(root)}
}

func (w Workspace) EffectivePlane() WorkspacePlane {
	if w.Plane == WorkspaceGlobal || w.Plane == WorkspaceUser {
		return WorkspaceGlobal
	}
	return WorkspaceLocal
}

func (w Workspace) SecretsPlane() secrets.SyncPlane {
	if w.EffectivePlane() == WorkspaceUser {
		return secrets.SyncPlaneMachine
	}
	return secrets.SyncPlaneProject
}

func (w Workspace) IDEPlane() ide.Plane {
	if w.EffectivePlane() == WorkspaceUser {
		return ide.PlaneUser
	}
	return ide.PlaneProject
}

// bundleScopeForPlane 返回该平面唯一可见的 bundle scope（ADR 0009 二元 scope）。
func bundleScopeForPlane(plane WorkspacePlane) types.BundleScope {
	if plane == WorkspaceUser {
		return types.BundleScopeUser
	}
	return types.BundleScopeProject
}

// loadWorkspaceBundleConfig 按平面读取已启用 bundle 列表（ADR 0009 平面隔离）。
//
// 用户平面的启用列表在 ~/.dec/config.yaml 的 enabled_bundles，这里只填充
// EnabledBundles，其余项目字段留空——用户平面没有 project vars / project secrets。
func loadWorkspaceBundleConfig(workspace Workspace) (*types.ProjectConfig, error) {
	if workspace.EffectivePlane() == WorkspaceUser {
		globalConfig, err := config.LoadGlobalConfig()
		if err != nil {
			return nil, err
		}
		return &types.ProjectConfig{EnabledBundles: append([]string(nil), globalConfig.EnabledBundles...)}, nil
	}
	return config.NewProjectConfigManager(workspace.Root).LoadProjectConfig()
}

// removeWorkspaceEnabledBundle 从当前平面的启用列表中摘掉一个 bundle。
// 返回是否发生变更。
func removeWorkspaceEnabledBundle(workspace Workspace, bundleName string) (bool, error) {
	bundleName = strings.TrimSpace(bundleName)
	if bundleName == "" {
		return false, nil
	}
	if workspace.EffectivePlane() == WorkspaceUser {
		globalConfig, err := config.LoadGlobalConfig()
		if err != nil {
			return false, err
		}
		updated, ok := removeEnabledBundle(globalConfig.EnabledBundles, bundleName)
		if !ok {
			return false, nil
		}
		globalConfig.EnabledBundles = updated
		if err := config.SaveGlobalConfig(globalConfig); err != nil {
			return false, err
		}
		return true, nil
	}
	mgr := config.NewProjectConfigManager(workspace.Root)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return false, err
	}
	updated, ok := removeEnabledBundle(projectConfig.EnabledBundles, bundleName)
	if !ok {
		return false, nil
	}
	projectConfig.EnabledBundles = updated
	if err := mgr.SaveProjectConfig(projectConfig); err != nil {
		return false, err
	}
	return true, nil
}
