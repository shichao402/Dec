package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/assets"
	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/ide"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/handler"
)

type LocalCleanupItem struct {
	Action   string
	Category string
	Path     string
	Detail   string
}

type LocalCleanupPreview struct {
	Items     []LocalCleanupItem
	Preserved []string
}

type LocalCleanupInput struct {
	Confirmed bool
}

type LocalCleanupResult struct {
	Deleted  []string
	Modified []string
	Revoked  []string
	Warnings []string
}

type cleanupScope struct {
	plane       ide.Plane
	projectRoot string
}

// PreviewLocalCleanup 只扫描当前布局中可证明由 Dec 管理的本机落点。
func PreviewLocalCleanup() (*LocalCleanupPreview, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	decHome, err := repo.GetRootDir()
	if err != nil {
		return nil, err
	}
	scopes, projects, err := cleanupScopes()
	if err != nil {
		return nil, err
	}
	preview := &LocalCleanupPreview{
		Preserved: []string{
			filepath.Join(decHome, "bin") + "（运行时四件套）",
			filepath.Join(decHome, "run") + "（当前服务实例）",
			"Dec Console",
		},
	}
	for _, project := range projects {
		for _, rel := range []string{secrets.IntegrationAuthRel, secrets.IntegrationDecHomeRel} {
			path := filepath.Join(project, filepath.FromSlash(rel))
			if pathExists(path) {
				preview.Preserved = append(preview.Preserved, path+"（集成测试）")
			}
		}
	}
	seen := map[string]struct{}{}
	add := func(item LocalCleanupItem) {
		key := item.Action + "\x00" + item.Path + "\x00" + item.Detail
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		preview.Items = append(preview.Items, item)
	}

	for _, scope := range scopes {
		for _, ideName := range ide.List() {
			impl := ide.Get(ideName)
			for _, candidate := range managedIDEPaths(impl, scope, home) {
				if pathExists(candidate.path) {
					add(LocalCleanupItem{Action: "delete", Category: candidate.category, Path: candidate.path})
				}
			}
			names, listErr := ide.DecMCPEntries(impl, scope.plane, scope.projectRoot, home)
			if listErr != nil {
				continue
			}
			if len(names) > 0 {
				add(LocalCleanupItem{
					Action: "modify", Category: "MCP 配置",
					Path:   impl.MCPConfigPathForPlane(scope.plane, scope.projectRoot, home),
					Detail: strings.Join(names, "、"),
				})
			}
		}
	}

	for _, project := range projects {
		if path := filepath.Join(project, ".dec"); pathExists(path) {
			add(LocalCleanupItem{Action: "delete", Category: "项目状态", Path: path})
		}
		if path := filepath.Join(project, ".secrets"); pathExists(path) {
			add(LocalCleanupItem{
				Action: "modify", Category: "项目 secrets", Path: path,
				Detail: "清空 Dec 落地；保留 integration 测试凭据与隔离 DEC_HOME",
			})
		}
		if paths, pathErr := secrets.ProjectCredentialScopePaths(project); pathErr == nil {
			hasGitFragment := pathExists(paths.GitFragment)
			for _, path := range []string{paths.GitFragment, paths.SSHFragment} {
				if pathExists(path) {
					add(LocalCleanupItem{Action: "delete", Category: "项目凭据配置", Path: path})
				}
			}
			if hasGitFragment {
				add(LocalCleanupItem{Action: "modify", Category: "Git 全局配置", Path: "~/.gitconfig", Detail: project})
			}
		}
	}

	if entries, readErr := os.ReadDir(decHome); readErr == nil {
		for _, entry := range entries {
			if entry.Name() == "bin" || entry.Name() == "run" {
				continue
			}
			add(LocalCleanupItem{Action: "delete", Category: "Dec 本机状态", Path: filepath.Join(decHome, entry.Name())})
		}
	}
	sshDir := filepath.Join(home, ".ssh")
	for _, pattern := range []string{"dec_*", "config.d/dec.conf", "config.d/dec-project-*.conf"} {
		matches, _ := filepath.Glob(filepath.Join(sshDir, filepath.FromSlash(pattern)))
		for _, path := range matches {
			add(LocalCleanupItem{Action: "delete", Category: "SSH", Path: path})
		}
	}
	if hasDecSSHInclude(filepath.Join(sshDir, "config")) {
		add(LocalCleanupItem{Action: "modify", Category: "SSH 主配置", Path: filepath.Join(sshDir, "config"), Detail: "移除 Include config.d/dec.conf"})
	}
	for _, item := range localGCMItems(decHome, projects) {
		add(LocalCleanupItem{Action: "revoke", Category: "Git Credential Manager", Path: item.path, Detail: item.name})
	}
	sort.Slice(preview.Items, func(i, j int) bool {
		if preview.Items[i].Category != preview.Items[j].Category {
			return preview.Items[i].Category < preview.Items[j].Category
		}
		return preview.Items[i].Path < preview.Items[j].Path
	})
	return preview, nil
}

// CleanupLocalInstallation 删除资产与本机状态，但保留 Console、bin 与当前 run。
func CleanupLocalInstallation(ctx context.Context, input LocalCleanupInput, reporter Reporter) (*LocalCleanupResult, error) {
	if !input.Confirmed {
		return nil, fmt.Errorf("清理本机需要 confirmed=true")
	}
	reporter = defaultReporter(reporter)
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	decHome, err := repo.GetRootDir()
	if err != nil {
		return nil, err
	}
	scopes, projects, err := cleanupScopes()
	if err != nil {
		return nil, err
	}
	result := &LocalCleanupResult{}
	warn := func(message string) {
		result.Warnings = append(result.Warnings, message)
		emit(reporter, EventWarn, "cleanup.local", message, nil)
	}

	for _, item := range localGCMItems(decHome, projects) {
		data, readErr := os.ReadFile(item.path)
		if readErr != nil {
			warn(fmt.Sprintf("读取 GCM 落地失败 %s: %v", item.path, readErr))
			continue
		}
		_, revokeErr := handler.RevokeNotes(ctx, nil, []handler.Item{{
			Source: handler.SourceNote, Name: item.name, NoteContent: string(data),
			ProjectRoot: item.projectRoot, ProjectScoped: item.projectRoot != "",
		}})
		if revokeErr != nil {
			warn(fmt.Sprintf("撤销 GCM 凭据失败 %s: %v", item.name, revokeErr))
		} else {
			result.Revoked = append(result.Revoked, item.name)
		}
	}

	for _, scope := range scopes {
		for _, ideName := range ide.List() {
			impl := ide.Get(ideName)
			for _, candidate := range managedIDEPaths(impl, scope, home) {
				if !pathExists(candidate.path) {
					continue
				}
				if removeErr := os.RemoveAll(candidate.path); removeErr != nil {
					warn(fmt.Sprintf("删除失败 %s: %v", candidate.path, removeErr))
				} else {
					result.Deleted = append(result.Deleted, candidate.path)
				}
			}
			names, removeErr := ide.RemoveDecMCPEntries(impl, scope.plane, scope.projectRoot, home)
			if removeErr != nil {
				warn(fmt.Sprintf("更新 %s MCP 配置失败: %v", ideName, removeErr))
			} else if len(names) > 0 {
				result.Modified = append(result.Modified, impl.MCPConfigPathForPlane(scope.plane, scope.projectRoot, home))
			}
		}
	}

	for _, project := range projects {
		if cleanupErr := secrets.CleanupProjectCredentialScope(project); cleanupErr != nil {
			warn(fmt.Sprintf("清理项目凭据配置失败 %s: %v", project, cleanupErr))
		}
		removeTracked(filepath.Join(project, ".dec"), result, warn)
		secretsRoot := filepath.Join(project, ".secrets")
		if pathExists(secretsRoot) {
			keeps := []string{
				filepath.Join(project, filepath.FromSlash(secrets.IntegrationAuthRel)),
				filepath.Join(project, filepath.FromSlash(secrets.IntegrationDecHomeRel)),
			}
			if cleanupErr := removeTreeExcept(secretsRoot, keeps); cleanupErr != nil {
				warn(fmt.Sprintf("清理项目 secrets 失败 %s: %v", project, cleanupErr))
			} else {
				result.Modified = append(result.Modified, secretsRoot)
			}
		}
	}
	sshConfigPath := filepath.Join(home, ".ssh", "config")
	hadSSHConfig := hasDecSSHInclude(sshConfigPath) || pathExists(filepath.Join(home, ".ssh", "config.d", "dec.conf"))
	if cleanupErr := secrets.CleanupManagedSSHConfig(); cleanupErr != nil {
		warn(fmt.Sprintf("清理 SSH 主配置失败: %v", cleanupErr))
	} else if hadSSHConfig {
		result.Modified = append(result.Modified, sshConfigPath)
	}
	sshDir := filepath.Join(home, ".ssh")
	for _, pattern := range []string{"dec_*", "config.d/dec-project-*.conf"} {
		matches, _ := filepath.Glob(filepath.Join(sshDir, filepath.FromSlash(pattern)))
		for _, path := range matches {
			removeTracked(path, result, warn)
		}
	}
	if entries, readErr := os.ReadDir(decHome); readErr == nil {
		for _, entry := range entries {
			if entry.Name() == "bin" || entry.Name() == "run" {
				continue
			}
			removeTracked(filepath.Join(decHome, entry.Name()), result, warn)
		}
	}
	emit(reporter, EventInfo, "cleanup.local",
		fmt.Sprintf("清理完成：删除 %d，修改 %d，撤销凭据 %d", len(result.Deleted), len(result.Modified), len(result.Revoked)), nil)
	return result, nil
}

func cleanupScopes() ([]cleanupScope, []string, error) {
	projects, err := config.ListManagedProjects()
	if err != nil {
		return nil, nil, err
	}
	scopes := []cleanupScope{{plane: ide.PlaneUser}}
	roots := make([]string, 0, len(projects))
	for _, project := range projects {
		roots = append(roots, project.Root)
		scopes = append(scopes, cleanupScope{plane: ide.PlaneProject, projectRoot: project.Root})
	}
	return scopes, roots, nil
}

type cleanupPath struct{ path, category string }

func managedIDEPaths(impl ide.IDE, scope cleanupScope, home string) []cleanupPath {
	var out []cleanupPath
	if scope.plane == ide.PlaneUser {
		builtin := assets.GlobalAssets()
		for _, skill := range builtin.Skills {
			out = append(out, cleanupPath{filepath.Join(impl.SkillsDirForPlane(scope.plane, scope.projectRoot, home), skill.Name), "内置 Skill"})
		}
	}
	for _, dir := range []struct {
		path, category string
	}{
		{impl.SkillsDirForPlane(scope.plane, scope.projectRoot, home), "Skill"},
		{impl.CommandsDirForPlane(scope.plane, scope.projectRoot, home), "Command"},
		{impl.RulesDirForPlane(scope.plane, scope.projectRoot, home), "Rule"},
	} {
		matches, _ := filepath.Glob(filepath.Join(dir.path, "dec-*"))
		for _, match := range matches {
			out = append(out, cleanupPath{match, dir.category})
		}
	}
	return out
}

type cleanupGCMItem struct{ path, name, projectRoot string }

func localGCMItems(decHome string, projects []string) []cleanupGCMItem {
	var out []cleanupGCMItem
	roots := []struct{ path, project string }{{filepath.Join(decHome, "secrets"), ""}}
	for _, project := range projects {
		roots = append(roots, struct{ path, project string }{filepath.Join(project, ".secrets"), project})
	}
	for _, root := range roots {
		protected := []string(nil)
		if root.project != "" {
			protected = []string{
				filepath.Join(root.project, filepath.FromSlash(secrets.IntegrationAuthRel)),
				filepath.Join(root.project, filepath.FromSlash(secrets.IntegrationDecHomeRel)),
			}
		}
		_ = filepath.Walk(root.path, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			for _, keep := range protected {
				if pathWithin(path, keep) {
					return nil
				}
			}
			slash := filepath.ToSlash(path)
			idx := strings.LastIndex(slash, "/.gcm/")
			if idx < 0 {
				return nil
			}
			out = append(out, cleanupGCMItem{path: path, name: slash[idx+1:], projectRoot: root.project})
			return nil
		})
	}
	return out
}

func removeTracked(path string, result *LocalCleanupResult, warn func(string)) {
	if !pathExists(path) {
		return
	}
	if err := os.RemoveAll(path); err != nil {
		warn(fmt.Sprintf("删除失败 %s: %v", path, err))
	} else {
		result.Deleted = append(result.Deleted, path)
	}
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func removeTreeExcept(root string, keeps []string) error {
	var existing []string
	for _, keep := range keeps {
		if pathExists(keep) && pathWithin(keep, root) {
			existing = append(existing, filepath.Clean(keep))
		}
	}
	if len(existing) == 0 {
		return os.RemoveAll(root)
	}
	var clean func(string) error
	clean = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			insideKeep := false
			parentOfKeep := false
			for _, keep := range existing {
				if pathWithin(path, keep) {
					insideKeep = true
				}
				if pathWithin(keep, path) {
					parentOfKeep = true
				}
			}
			if insideKeep {
				continue
			}
			if !parentOfKeep || !entry.IsDir() {
				if err := os.RemoveAll(path); err != nil {
					return err
				}
				continue
			}
			if err := clean(path); err != nil {
				return err
			}
		}
		return nil
	}
	return clean(root)
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func hasDecSSHInclude(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "include") &&
			strings.HasSuffix(strings.ReplaceAll(strings.Trim(fields[1], `"'`), "\\", "/"), "config.d/dec.conf") {
			return true
		}
	}
	return false
}
