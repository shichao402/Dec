package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/secrets"
)

// AddSecretResult 是一次「登记新 secret」的结果。
type AddSecretResult struct {
	Kind           secrets.SyncKind
	TargetName     string
	Folder         string
	NoteRelPath    string // 相对同步根
	ProjectRelPath string // 项目根相对
	// LandingPath 兼容旧 TUI/测试，等于 ProjectRelPath。
	LandingPath string
}

// SecretTargetOption 是 TUI 可选的登记归属。
type SecretTargetOption struct {
	Kind      secrets.SyncKind
	Name      string
	Folder    string
	LocalRoot string
	Label     string
}

// AddProjectSecret 兼容旧签名：folder + 路径。
// folder 可为 `bundle/<名>` 或短名；若 relPath 以同步根为前缀则剥成 note 相对路径。
func AddProjectSecret(ctx context.Context, projectRoot, folder, relPath string, reporter Reporter) (*AddSecretResult, error) {
	opts, err := SuggestSecretTargets(projectRoot)
	if err != nil {
		return nil, err
	}
	folder = strings.Trim(strings.TrimSpace(strings.ReplaceAll(folder, "\\", "/")), "/")
	var target secrets.SyncTarget
	found := false
	for _, opt := range opts {
		if opt.Kind != secrets.SyncKindBundle {
			continue
		}
		if opt.Folder == folder || opt.Name == folder ||
			strings.TrimPrefix(opt.Folder, secrets.BundleFolderPrefix) == folder {
			target, err = secrets.NewBundleSyncTarget(opt.Name, opt.Folder)
			if err != nil {
				return nil, err
			}
			found = true
			break
		}
	}
	if !found {
		name := strings.TrimPrefix(folder, secrets.BundleFolderPrefix)
		if !strings.HasPrefix(folder, secrets.BundleFolderPrefix) {
			return nil, fmt.Errorf("未知 secrets 归属 folder %q；请使用 bundle/<名>", folder)
		}
		t, err := secrets.NewBundleSyncTarget(name, folder)
		if err != nil {
			return nil, fmt.Errorf("未知 secrets 归属 folder %q", folder)
		}
		target = t
	}

	rel := filepath.ToSlash(strings.TrimSpace(relPath))
	prefix := strings.Trim(target.LocalRoot, "/") + "/"
	if strings.HasPrefix(rel, prefix) {
		rel = strings.TrimPrefix(rel, prefix)
	}
	return AddSecretToTarget(ctx, projectRoot, target, rel, reporter)
}

// AddSecretToTarget 把 SyncTarget 同步根下已存在的文件登记为 Secure Note。
func AddSecretToTarget(ctx context.Context, projectRoot string, target secrets.SyncTarget, noteRel string, reporter Reporter) (*AddSecretResult, error) {
	reporter = defaultReporter(reporter)
	if target.Kind == secrets.SyncKindProject {
		return nil, fmt.Errorf("ADR 0014：project 级 secrets 已取消；请写入 bundle/<名>")
	}
	if err := secrets.RequireDeclared(target); err != nil {
		return nil, err
	}
	if target.Folder == "" || target.LocalRoot == "" {
		return nil, fmt.Errorf("必须指定 secrets 归属 SyncTarget")
	}
	noteRel = filepath.ToSlash(strings.TrimSpace(noteRel))
	if noteRel == "" {
		return nil, fmt.Errorf("必须指定相对同步根的路径（如 .env/vikunja.env）")
	}

	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		return nil, fmt.Errorf("Bitwarden 未配置，请先在 Settings 页填写连接信息")
	}

	abs, err := secrets.AbsolutePath(projectRoot, target, noteRel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s 不存在：请先写到 %s/ 下再登记", noteRel, target.LocalRoot)
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s 是目录：一个 Secure Note 对应一个文件，请逐个登记", noteRel)
	}

	if !secrets.HasSession() {
		if err := ensureBitwardenSession(ctx, reporter, "secrets.add"); err != nil {
			return nil, err
		}
	}

	if err := secrets.AddSecureNote(ctx, secretsClientFactory(), projectRoot, target, noteRel); err != nil {
		return nil, err
	}
	projectRel, _ := secrets.ProjectRelPath(target, noteRel)
	emit(reporter, EventInfo, "secrets.add",
		fmt.Sprintf("已登记 %s → Bitwarden folder %q（本地 %s）", noteRel, target.Folder, projectRel), nil)
	return &AddSecretResult{
		Kind:           target.Kind,
		TargetName:     target.Name,
		Folder:         target.Folder,
		NoteRelPath:    noteRel,
		ProjectRelPath: projectRel,
		LandingPath:    projectRel,
	}, nil
}

// SuggestSecretTargets 列出可选登记归属（仅已启用的 bundle SyncTarget，ADR 0014）。
func SuggestSecretTargets(projectRoot string) ([]SecretTargetOption, error) {
	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	cfg, err := secrets.LoadConfig()
	if err != nil {
		return nil, err
	}
	plan, err := planSecretsSync(projectRoot, projectConfig.EnabledBundles, cfg)
	if err != nil {
		return nil, err
	}

	opts := make([]SecretTargetOption, 0, len(plan.Targets))
	for _, t := range plan.Targets {
		if t.Kind != secrets.SyncKindBundle {
			continue
		}
		opts = append(opts, SecretTargetOption{
			Kind:      t.Kind,
			Name:      t.Name,
			Folder:    t.Folder,
			LocalRoot: t.LocalRoot,
			Label:     formatSyncTargetLabel(t) + " → " + t.LocalRoot,
		})
	}
	sort.Slice(opts, func(i, j int) bool { return opts[i].Name < opts[j].Name })
	return opts, nil
}

// ListSecretSyncTargets 返回 pull/push 将涉及的 SyncTarget 摘要（供 TUI Run 页展示）。
func ListSecretSyncTargets(projectRoot string) ([]SecretTargetOption, error) {
	return SuggestSecretTargets(projectRoot)
}

// SuggestSecretFolders 兼容旧 API，返回 folder 名列表（仅 enabled bundles）。
func SuggestSecretFolders(projectRoot string) ([]string, error) {
	opts, err := SuggestSecretTargets(projectRoot)
	if err != nil {
		return nil, err
	}
	folders := make([]string, 0, len(opts))
	for _, opt := range opts {
		folders = append(folders, opt.Folder)
	}
	return folders, nil
}
