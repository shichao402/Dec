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

// CleanupTier 是本机清理的档位。只有两档，避免出现无法说清边界的中间态。
type CleanupTier string

const (
	// CleanupTierLight 只清掉程序可以重新拉回来的产物，保留用户手工积累的配置与凭据。
	CleanupTierLight CleanupTier = "light"
	// CleanupTierFull 清掉除 Console、运行时套件与当前服务实例之外的全部 Dec 落点。
	CleanupTierFull CleanupTier = "full"

	// LocalCleanupConfirmPhrase 是彻底清理沿用的确认词。
	LocalCleanupConfirmPhrase = "清理本机"
	// LocalCleanupLightConfirmPhrase 是轻量清理的确认词，与彻底清理区分，避免误确认。
	LocalCleanupLightConfirmPhrase = "只清理资产"
)

// ParseCleanupTier 解析档位；空值按 full 执行，保持引入档位前的调用方行为不变。
func ParseCleanupTier(raw string) (CleanupTier, error) {
	switch CleanupTier(strings.ToLower(strings.TrimSpace(raw))) {
	case "":
		return CleanupTierFull, nil
	case CleanupTierLight:
		return CleanupTierLight, nil
	case CleanupTierFull:
		return CleanupTierFull, nil
	default:
		return "", fmt.Errorf("未知的清理档位 %q，只支持 light 或 full", raw)
	}
}

// ConfirmPhrase 返回该档位要求的确认词。
func (t CleanupTier) ConfirmPhrase() string {
	if t == CleanupTierLight {
		return LocalCleanupLightConfirmPhrase
	}
	return LocalCleanupConfirmPhrase
}

func (t CleanupTier) label() string {
	if t == CleanupTierLight {
		return "轻量清理"
	}
	return "彻底清理"
}

type LocalCleanupItem struct {
	Action   string
	Category string
	Path     string
	Detail   string
}

type LocalCleanupPreview struct {
	Tier  CleanupTier
	Items []LocalCleanupItem
	// Preserved 是本档位不会动到的落点。
	Preserved []string
	// KeptByTier 是本档位保留、但换到彻底清理会被删除的条目。
	KeptByTier []LocalCleanupItem
}

type LocalCleanupInput struct {
	Confirmed bool
	// Tier 为空等价于 full，保持旧调用方行为。
	Tier string
}

type LocalCleanupResult struct {
	Tier     CleanupTier
	Deleted  []string
	Modified []string
	Revoked  []string
	Warnings []string
	// Preserved 列出本档位保留的用户手工积累项，供结果摘要使用。
	Preserved []string
	// NextAction 是清理完成后恢复资产的建议动作。
	NextAction string
}

type cleanupScope struct {
	plane       ide.Plane
	projectRoot string
}

// 落点类别。两档共用这一组常量，避免出现两套互相漂移的清单。
const (
	cleanupCategoryIDESkill     = "Skill"
	cleanupCategoryIDECommand   = "Command"
	cleanupCategoryIDERule      = "Rule"
	cleanupCategoryBuiltin      = "内置 Skill"
	cleanupCategoryMCP          = "MCP 配置"
	cleanupCategoryProjectSt    = "项目状态"
	cleanupCategoryProjectSec   = "项目 secrets"
	cleanupCategoryProjCreds    = "项目凭据配置"
	cleanupCategoryProjectCache = "项目缓存"
	cleanupCategoryGitGlobal    = "Git 全局配置"
	cleanupCategoryDecHome      = "Dec 本机状态"
	cleanupCategorySSH          = "SSH"
	cleanupCategorySSHMain      = "SSH 主配置"
	cleanupCategoryGitCredMgr   = "Git Credential Manager"
)

// isKeptByLight 判断某一类落点在轻量清理下是否保留。
// 判定与落点枚举共用同一类别来源，不另写第二套扫描逻辑。
func isKeptByLight(category, path string) bool {
	switch category {
	case cleanupCategoryProjectSt, cleanupCategoryProjectSec, cleanupCategoryProjCreds,
		cleanupCategoryGitGlobal, cleanupCategorySSH, cleanupCategorySSHMain,
		cleanupCategoryGitCredMgr:
		return true
	case cleanupCategoryDecHome:
		// ~/.dec 下 bin/run 已被排除；cache 与 repo.git 属于可重新生成的产物，
		// 轻量清理也要清掉，其余（config.yaml、secrets、pruned 等）保留。
		switch filepath.Base(path) {
		case "cache", "repo.git":
			return false
		default:
			return true
		}
	case cleanupCategoryProjectCache:
		// 项目级 .dec/cache 同样是可重新生成的安装缓存，两档都清。
		return false
	default:
		return false
	}
}

// retainedReason 给出轻量清理保留某类落点的理由，供预览与结果摘要展示。
func retainedReason(category, path string) string {
	base := filepath.Base(path)
	switch category {
	case cleanupCategoryProjectSt:
		return "项目配置与资产声明（requires 等仍要生效）"
	case cleanupCategoryProjectSec:
		return "本地 secret 落地与集成测试凭据"
	case cleanupCategoryProjCreds:
		return "项目 Git/SSH 凭据片段"
	case cleanupCategoryGitGlobal:
		return "全局 Git 配置中的 Dec 条目"
	case cleanupCategorySSH:
		return "Dec 管理的 SSH 密钥与配置片段"
	case cleanupCategorySSHMain:
		return "SSH 主配置中的 Dec Include"
	case cleanupCategoryGitCredMgr:
		return "Git Credential Manager 中的 Dec 凭据"
	case cleanupCategoryDecHome:
		switch base {
		case "config.yaml":
			return "全局配置：私仓地址、受管项目、IDE 选择与资产启用列表"
		case "secrets":
			return "本地 secret 落地"
		case "pruned":
			return "隔离区内容"
		default:
			return "Dec 本机状态，轻量清理不触碰"
		}
	default:
		return ""
	}
}

// describePreserved 把一条保留条目拼成可读文本，路径为空时只给理由。
func describePreserved(path, reason string) string {
	if strings.TrimSpace(path) == "" {
		return reason
	}
	if reason == "" {
		return path
	}
	return path + "（" + reason + "）"
}

// previewFacts 一次扫描得到的原始落点，供预览与执行共用。
type cleanupFacts struct {
	scopes   []cleanupScope
	projects []string
	items    []LocalCleanupItem
}

func collectCleanupFacts() (*cleanupFacts, error) {
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
	facts := &cleanupFacts{scopes: scopes, projects: projects}
	seen := map[string]struct{}{}
	add := func(item LocalCleanupItem) {
		key := item.Action + "\x00" + item.Path + "\x00" + item.Detail
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		facts.items = append(facts.items, item)
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
					Action: "modify", Category: cleanupCategoryMCP,
					Path:   impl.MCPConfigPathForPlane(scope.plane, scope.projectRoot, home),
					Detail: strings.Join(names, "、"),
				})
			}
		}
	}

	for _, project := range projects {
		if path := filepath.Join(project, ".dec", "cache"); pathExists(path) {
			add(LocalCleanupItem{Action: "delete", Category: cleanupCategoryProjectCache, Path: path})
		}
		if path := filepath.Join(project, ".dec"); pathExists(path) {
			add(LocalCleanupItem{Action: "delete", Category: cleanupCategoryProjectSt, Path: path})
		}
		if path := filepath.Join(project, ".secrets"); pathExists(path) {
			add(LocalCleanupItem{
				Action: "modify", Category: cleanupCategoryProjectSec, Path: path,
				Detail: "清空 Dec 落地；保留 integration 测试凭据与隔离 DEC_HOME",
			})
		}
		if paths, pathErr := secrets.ProjectCredentialScopePaths(project); pathErr == nil {
			hasGitFragment := pathExists(paths.GitFragment)
			for _, path := range []string{paths.GitFragment, paths.SSHFragment} {
				if pathExists(path) {
					add(LocalCleanupItem{Action: "delete", Category: cleanupCategoryProjCreds, Path: path})
				}
			}
			if hasGitFragment {
				add(LocalCleanupItem{Action: "modify", Category: cleanupCategoryGitGlobal, Path: "~/.gitconfig", Detail: project})
			}
		}
	}

	if entries, readErr := os.ReadDir(decHome); readErr == nil {
		for _, entry := range entries {
			if entry.Name() == "bin" || entry.Name() == "run" {
				continue
			}
			add(LocalCleanupItem{Action: "delete", Category: cleanupCategoryDecHome, Path: filepath.Join(decHome, entry.Name())})
		}
	}
	sshDir := filepath.Join(home, ".ssh")
	for _, pattern := range []string{"dec_*", "config.d/dec.conf", "config.d/dec-project-*.conf"} {
		matches, _ := filepath.Glob(filepath.Join(sshDir, filepath.FromSlash(pattern)))
		for _, path := range matches {
			add(LocalCleanupItem{Action: "delete", Category: cleanupCategorySSH, Path: path})
		}
	}
	if hasDecSSHInclude(filepath.Join(sshDir, "config")) {
		add(LocalCleanupItem{
			Action: "modify", Category: cleanupCategorySSHMain,
			Path:   filepath.Join(sshDir, "config"),
			Detail: "移除 Include config.d/dec.conf",
		})
	}
	for _, item := range localGCMItems(decHome, projects) {
		add(LocalCleanupItem{Action: "revoke", Category: cleanupCategoryGitCredMgr, Path: item.path, Detail: item.name})
	}
	sort.Slice(facts.items, func(i, j int) bool {
		if facts.items[i].Category != facts.items[j].Category {
			return facts.items[i].Category < facts.items[j].Category
		}
		return facts.items[i].Path < facts.items[j].Path
	})
	return facts, nil
}

// alwaysPreserved 是两个档位都不动的落点。
func alwaysPreserved(projects []string) []string {
	out := []string{"Dec Console（本机程序壳，不被任何档位清理）"}
	if decHome, err := repo.GetRootDir(); err == nil {
		out = append(out,
			filepath.Join(decHome, "bin")+"（运行时套件）",
			filepath.Join(decHome, "run")+"（当前服务实例）",
		)
	}
	for _, project := range projects {
		for _, rel := range []string{secrets.IntegrationAuthRel, secrets.IntegrationDecHomeRel} {
			path := filepath.Join(project, filepath.FromSlash(rel))
			if pathExists(path) {
				out = append(out, path+"（集成测试）")
			}
		}
	}
	return out
}

// PreviewLocalCleanup 只扫描当前布局中可证明由 Dec 管理的本机落点。
func PreviewLocalCleanup(tier CleanupTier) (*LocalCleanupPreview, error) {
	if tier == "" {
		tier = CleanupTierFull
	}
	facts, err := collectCleanupFacts()
	if err != nil {
		return nil, err
	}
	preview := &LocalCleanupPreview{
		Tier:      tier,
		Items:     []LocalCleanupItem{},
		Preserved: alwaysPreserved(facts.projects),
	}
	for _, item := range facts.items {
		if tier == CleanupTierLight && isKeptByLight(item.Category, item.Path) {
			if reason := retainedReason(item.Category, item.Path); reason != "" {
				kept := item
				kept.Detail = strings.TrimSpace(strings.Join([]string{item.Detail, reason}, " "))
				preview.KeptByTier = append(preview.KeptByTier, kept)
				preview.Preserved = append(preview.Preserved, describePreserved(item.Path, reason))
			}
			continue
		}
		preview.Items = append(preview.Items, item)
	}
	return preview, nil
}

// CleanupLocalInstallation 删除资产与本机状态，但保留 Console、bin 与当前 run。
// tier 为 light 时额外保留配置与凭据面。
func CleanupLocalInstallation(ctx context.Context, input LocalCleanupInput, reporter Reporter) (*LocalCleanupResult, error) {
	tier, err := ParseCleanupTier(input.Tier)
	if err != nil {
		return nil, err
	}
	if !input.Confirmed {
		return nil, fmt.Errorf("%s需要 confirmed=true，且确认词为“%s”", tier.label(), tier.ConfirmPhrase())
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
	result := &LocalCleanupResult{Tier: tier}
	result.Preserved = alwaysPreserved(projects)
	if tier == CleanupTierLight {
		result.NextAction = "配置与凭据都已保留，执行一次拉取即可把资产恢复可用。"
	}
	warn := func(message string) {
		result.Warnings = append(result.Warnings, message)
		emit(reporter, EventWarn, "cleanup.local", message, nil)
	}
	kept := func(items []LocalCleanupItem) {
		if tier != CleanupTierLight {
			return
		}
		for _, item := range items {
			if reason := retainedReason(item.Category, item.Path); reason != "" {
				result.Preserved = append(result.Preserved, describePreserved(item.Path, reason))
			}
		}
	}
	emit(reporter, EventInfo, "cleanup.local",
		fmt.Sprintf("开始%s", tier.label()), nil)

	// 凭据撤销只在彻底清理下发生；轻量清理保留 GCM 里的 Dec 凭据。
	if tier == CleanupTierFull {
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
	} else {
		kept([]LocalCleanupItem{{Category: cleanupCategoryGitCredMgr}})
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

	if tier == CleanupTierFull {
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
	} else {
		// 轻量清理保留项目状态与凭据，但各级 cache 属于可重新生成的产物，需要清掉。
		for _, project := range projects {
			removeTracked(filepath.Join(project, ".dec", "cache"), result, warn)
		}
		kept([]LocalCleanupItem{
			{Category: cleanupCategoryProjectSt},
			{Category: cleanupCategoryProjectSec},
			{Category: cleanupCategoryProjCreds},
			{Category: cleanupCategoryGitGlobal, Path: "~/.gitconfig"},
			{Category: cleanupCategorySSH},
			{Category: cleanupCategorySSHMain, Path: filepath.Join(home, ".ssh", "config")},
		})
	}

	if entries, readErr := os.ReadDir(decHome); readErr == nil {
		for _, entry := range entries {
			if entry.Name() == "bin" || entry.Name() == "run" {
				continue
			}
			path := filepath.Join(decHome, entry.Name())
			category := cleanupCategoryDecHome
			if tier == CleanupTierLight && isKeptByLight(category, path) {
				if reason := retainedReason(category, path); reason != "" {
					result.Preserved = append(result.Preserved, describePreserved(path, reason))
				}
				continue
			}
			removeTracked(path, result, warn)
		}
	}
	emit(reporter, EventInfo, "cleanup.local",
		fmt.Sprintf("%s完成：删除 %d，修改 %d，撤销凭据 %d，保留 %d",
			tier.label(), len(result.Deleted), len(result.Modified), len(result.Revoked), len(result.Preserved)), nil)
	if result.NextAction != "" {
		emit(reporter, EventInfo, "cleanup.local", result.NextAction, nil)
	}
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
			out = append(out, cleanupPath{filepath.Join(impl.SkillsDirForPlane(scope.plane, scope.projectRoot, home), skill.Name), cleanupCategoryBuiltin})
		}
	}
	for _, dir := range []struct {
		path, category string
	}{
		{impl.SkillsDirForPlane(scope.plane, scope.projectRoot, home), cleanupCategoryIDESkill},
		{impl.CommandsDirForPlane(scope.plane, scope.projectRoot, home), cleanupCategoryIDECommand},
		{impl.RulesDirForPlane(scope.plane, scope.projectRoot, home), cleanupCategoryIDERule},
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
	var roots []struct{ path, project string }
	// decHome 为空时只扫项目 .secrets（单项目 purge），不碰本机 secrets 根。
	if decHome != "" {
		roots = append(roots, struct{ path, project string }{filepath.Join(decHome, "secrets"), ""})
	}
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
