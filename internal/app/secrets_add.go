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
	TargetName     string
	Address        string // 远端逻辑地址（展示用）
	NoteRelPath    string // 相对同步根
	ProjectRelPath string // 项目根相对
	// LandingPath 兼容旧 TUI/测试，等于 ProjectRelPath。
	LandingPath string
}

// SecretTargetOption 是 TUI 可选的登记归属。
type SecretTargetOption struct {
	Name      string
	Plane     secrets.SyncPlane
	Address   string
	LocalRoot string
	Label     string
}

// AddProjectSecretForScope 按 (P, 平面) 登记同步根下已存在的文件。
// relPath 允许带同步根前缀，会被剥成 note 相对路径。
func AddProjectSecretForScope(ctx context.Context, projectRoot string, scope secrets.RemoteScope, relPath string, reporter Reporter) (*AddSecretResult, error) {
	target, err := secrets.NewPSyncTarget(scope.P, scope.Plane)
	if err != nil {
		return nil, err
	}
	rel := filepath.ToSlash(strings.TrimSpace(relPath))
	prefix := strings.Trim(target.LocalRoot, "/") + "/"
	rel = strings.TrimPrefix(rel, prefix)
	return AddSecretToTarget(ctx, projectRoot, target, rel, reporter)
}

// AddSecretToTarget 把 SyncTarget 同步根下已存在的文件登记为 Secure Note。
func AddSecretToTarget(ctx context.Context, projectRoot string, target secrets.SyncTarget, noteRel string, reporter Reporter) (*AddSecretResult, error) {
	reporter = defaultReporter(reporter)
	if err := secrets.RequireDeclared(target); err != nil {
		return nil, err
	}
	if target.Address == "" || target.LocalRoot == "" {
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
		fmt.Sprintf("已登记 %s → %s（本地 %s）", noteRel, target.Address, projectRel), nil)
	return &AddSecretResult{
		TargetName:     target.Name,
		Address:        target.Address,
		NoteRelPath:    noteRel,
		ProjectRelPath: projectRel,
		LandingPath:    projectRel,
	}, nil
}

// SuggestSecretTargets 列出可选登记归属（当前平面已启用 P 的 SyncTarget）。
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
		opts = append(opts, SecretTargetOption{
			Name:      t.Name,
			Plane:     t.Plane,
			Address:   t.Address,
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

// SuggestSecretAddresses 返回可登记的远端逻辑地址列表（仅展示用）。
func SuggestSecretAddresses(projectRoot string) ([]string, error) {
	opts, err := SuggestSecretTargets(projectRoot)
	if err != nil {
		return nil, err
	}
	addresses := make([]string, 0, len(opts))
	for _, opt := range opts {
		addresses = append(addresses, opt.Address)
	}
	return addresses, nil
}
