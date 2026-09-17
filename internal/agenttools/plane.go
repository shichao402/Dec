package agenttools

import (
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/app"
)

func parsePlanes(raw string) ([]app.WorkspacePlane, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "project", "local":
		return []app.WorkspacePlane{app.WorkspaceLocal}, nil
	case "user", "global":
		return []app.WorkspacePlane{app.WorkspaceGlobal}, nil
	case "both":
		return []app.WorkspacePlane{app.WorkspaceLocal, app.WorkspaceGlobal}, nil
	default:
		return nil, fmt.Errorf("plane 非法 %q（允许 local|global|both，以及旧名 project|user）", raw)
	}
}

func parseSinglePlane(raw string) (app.WorkspacePlane, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "project", "local":
		return app.WorkspaceLocal, nil
	case "user", "global":
		return app.WorkspaceGlobal, nil
	case "both":
		return "", fmt.Errorf("该操作不支持 plane=both，请显式指定 local 或 global")
	default:
		return "", fmt.Errorf("plane 非法 %q（允许 local|global，以及旧名 project|user）", raw)
	}
}

func resolveWorkspace(plane app.WorkspacePlane, projectRoot string) (root string, err error) {
	root = strings.TrimSpace(projectRoot)
	if strings.Contains(root, "${") {
		root = ""
	}
	if plane == app.WorkspaceGlobal {
		return "", nil
	}
	if root == "" {
		return "", fmt.Errorf("plane=local 需要 project_root。请先用 dec_list_managed_projects 选定受管项目，或传入绝对路径")
	}
	return root, nil
}
