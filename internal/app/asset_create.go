package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

type LocalAssetKind struct {
	ID        string
	Label     string
	Sensitive bool
}

func LocalAssetKinds() []LocalAssetKind {
	out := []LocalAssetKind{
		{ID: "skill", Label: "skill（目录 · SKILL.md）"},
		{ID: "rule", Label: "rule（.mdc）"},
		{ID: "mcp", Label: "mcp（.json）"},
		{ID: "command", Label: "command（目录）"},
	}
	for _, p := range secrets.RegisteredProcessors() {
		out = append(out, LocalAssetKind{ID: string(p.ID), Label: p.Label, Sensitive: true})
	}
	return out
}

type CreateLocalAssetInput struct {
	Workspace  Workspace
	Project    string
	Kind       string
	Name       string
	Visibility types.AssetVisibility
	Plane      types.AssetPlane
}

type CreateLocalAssetResult struct {
	Path string
}

func CreateLocalAsset(in CreateLocalAssetInput) (*CreateLocalAssetResult, error) {
	name := strings.TrimSpace(in.Name)
	project := strings.TrimSpace(in.Project)
	if !types.IsValidProjectName(project) {
		return nil, fmt.Errorf("项目名 %q 非法", in.Project)
	}
	if name == "" {
		return nil, fmt.Errorf("名称不能为空")
	}
	vis := in.Visibility
	if vis == "" {
		vis = types.AssetVisibilityPrivate
	}
	plane := types.CanonicalAssetPlane(in.Plane)
	if in.Workspace.EffectivePlane() == WorkspaceGlobal {
		plane = types.AssetPlaneGlobal
	}

	kind, ok := bundle.KindByType(in.Kind)
	if ok {
		path, err := writeGitAsset(in.Workspace, project, vis, plane, kind, name)
		if err != nil {
			return nil, err
		}
		return &CreateLocalAssetResult{Path: path}, nil
	}
	proc, ok := secrets.LookupProcessor(in.Kind)
	if !ok {
		return nil, fmt.Errorf("未知类型 %q", in.Kind)
	}
	path, err := writeSecretAsset(in.Workspace, project, plane, proc, name)
	if err != nil {
		return nil, err
	}
	return &CreateLocalAssetResult{Path: path}, nil
}

func writeGitAsset(workspace Workspace, project string, vis types.AssetVisibility, plane types.AssetPlane, kind bundle.VaultAssetKind, name string) (string, error) {
	dir := filepath.Join(workspaceCacheDir(workspace), project, string(vis), string(plane), kind.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fileName := bundle.AssetFileName(kind, name)
	dest := filepath.Join(dir, fileName)
	if kind.DirEntries {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return "", err
		}
		file := "SKILL.md"
		if kind.Type == "command" {
			file = "run.md"
		}
		md := filepath.Join(dest, file)
		body := fmt.Sprintf("# %s\n", name)
		if err := os.WriteFile(md, []byte(body), 0o644); err != nil {
			return "", err
		}
		return md, nil
	}
	body := ""
	switch kind.Type {
	case "rule":
		body = "---\ndescription: " + name + "\n---\n"
	case "mcp":
		body = "{\n  \"mcpServers\": {}\n}\n"
	}
	if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

func writeSecretAsset(workspace Workspace, project string, plane types.AssetPlane, proc secrets.Processor, name string) (string, error) {
	syncPlane := secrets.SyncPlaneLocal
	if plane == types.AssetPlaneGlobal {
		syncPlane = secrets.SyncPlaneGlobal
	}
	target, err := secrets.NewPSyncTarget(project, syncPlane)
	if err != nil {
		return "", err
	}
	root, err := secrets.ResolveAbsDir(workspace.Root, target)
	if err != nil {
		return "", err
	}
	rel, err := proc.NormalizeName(name)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	body := proc.Template
	if body == "" {
		body = "\n"
	}
	if err := os.WriteFile(dest, []byte(body), 0o600); err != nil {
		return "", err
	}
	if proc.ID == secrets.SecretTypeEnv {
		if _, err := secrets.ParseDotEnvFile(dest); err != nil {
			_ = os.Remove(dest)
			return "", err
		}
	}
	if proc.ID == secrets.SecretTypeGCM {
		var parsed any
		if err := yaml.Unmarshal([]byte(body), &parsed); err != nil {
			_ = os.Remove(dest)
			return "", fmt.Errorf(".gcm YAML 无效: %w", err)
		}
	}
	return dest, nil
}
