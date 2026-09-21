package app

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/contribute"
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
	Mode string
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
		path, mode, err := writeGitAsset(in.Workspace, project, vis, plane, kind, name)
		if err != nil {
			return nil, err
		}
		return &CreateLocalAssetResult{Path: path, Mode: mode}, nil
	}
	proc, ok := secrets.LookupProcessor(in.Kind)
	if !ok {
		return nil, fmt.Errorf("未知类型 %q", in.Kind)
	}
	path, err := writeSecretAsset(in.Workspace, project, plane, proc, name)
	if err != nil {
		return nil, err
	}
	return &CreateLocalAssetResult{Path: path, Mode: "secret"}, nil
}

func writeGitAsset(workspace Workspace, project string, vis types.AssetVisibility, plane types.AssetPlane, kind bundle.VaultAssetKind, name string) (string, string, error) {
	if root := providerWriteRoot(workspace, project); root != "" {
		path, err := writeProvideAsset(root, vis, plane, kind, name)
		return path, "provides", err
	}
	if workspace.EffectivePlane() != WorkspaceGlobal && workspace.Root != "" {
		cfg, err := config.NewProjectConfigManager(workspace.Root).LoadProjectConfig()
		if err == nil && cfg != nil {
			req, _ := workspaceOfficialRequires(workspace, cfg)
			if req.Has(project) && !types.IsVaultPin(req[project]) {
				dir := contribute.DraftDir(workspace.Root, project+"-"+name)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return "", "", err
				}
				dest := filepath.Join(dir, bundle.AssetFileName(kind, name))
				path, err := writeGitAssetBody(kind, name, dest)
				return path, "draft", err
			}
		}
	}
	dir := filepath.Join(workspaceCacheDir(workspace), project, string(vis), string(plane), kind.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	dest := filepath.Join(dir, bundle.AssetFileName(kind, name))
	path, err := writeGitAssetBody(kind, name, dest)
	return path, "cache", err
}

func providerWriteRoot(workspace Workspace, project string) string {
	if workspace.Root != "" {
		cfg, err := config.NewProjectConfigManager(workspace.Root).LoadProjectConfig()
		if err == nil && cfg != nil && strings.TrimSpace(cfg.ProjectName) == project &&
			(len(cfg.Provides) > 0 || strings.TrimSpace(cfg.ProvidesRoot) != "") {
			return workspace.Root
		}
	}
	if vaultProjectAccess(project) == types.ProviderAccessDirect {
		return ProviderAuthorRoot(project)
	}
	return ""
}

func writeProvideAsset(root string, vis types.AssetVisibility, plane types.AssetPlane, kind bundle.VaultAssetKind, name string) (string, error) {
	mgr := config.NewProjectConfigManager(root)
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		return "", err
	}
	if cfg == nil {
		return "", fmt.Errorf("提供方工作区缺少 .dec/config.yaml")
	}
	dir := filepath.Join(root, filepath.FromSlash(config.ProvideAuthorDir(cfg.ProvidesRoot, kind.Dir)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, bundle.AssetFileName(kind, name))
	path, err := writeGitAssetBody(kind, name, dest)
	if err != nil {
		return "", err
	}
	if err := registerProjectProvide(mgr, cfg, vis, plane, kind, name); err != nil {
		return "", err
	}
	return path, nil
}

func registerProjectProvide(mgr *config.ProjectConfigManager, cfg *types.ProjectConfig, vis types.AssetVisibility, plane types.AssetPlane, kind bundle.VaultAssetKind, name string) error {
	key := kind.Type + "-" + name
	source := filepath.ToSlash(path.Join(config.ProvideAuthorDir(cfg.ProvidesRoot, kind.Dir), bundle.AssetFileName(kind, name)))
	if cfg.Provides == nil {
		cfg.Provides = map[string]types.ProjectProvide{}
	}
	cfg.Provides[key] = types.ProjectProvide{
		Source:     source,
		Visibility: vis,
		Plane:      plane,
		Type:       kind.Type,
		Name:       name,
	}
	return mgr.SaveProjectConfig(cfg)
}

func writeGitAssetBody(kind bundle.VaultAssetKind, name, dest string) (string, error) {
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
