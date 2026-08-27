package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

const PMigrationJournalVersion = 1

type PMigrationIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Source   string `json:"source,omitempty"`
	Target   string `json:"target,omitempty"`
	Message  string `json:"message"`
}

type PMigrationGitMove struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type PMigrationBWMove struct {
	SourceFolder string `json:"source_folder"`
	TargetFolder string `json:"target_folder"`
	Path         string `json:"path"`
	Kind         string `json:"kind"` // note | sshkey
}

type PMigrationManifest struct {
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Requires    []string `json:"requires,omitempty"`
	IDEs        []string `json:"ides,omitempty"`
	Editor      string   `json:"editor,omitempty"`
}

// PMigrationPlan 是完全只读的迁移预览结果，不包含任何 Bitwarden 正文。
type PMigrationPlan struct {
	Version        int                  `json:"version"`
	Fingerprint    string               `json:"fingerprint"`
	LegacyDetected bool                 `json:"legacy_detected"`
	Manifests      []PMigrationManifest `json:"manifests,omitempty"`
	GitMoves       []PMigrationGitMove  `json:"git_moves,omitempty"`
	BWMoves        []PMigrationBWMove   `json:"bw_moves,omitempty"`
	Issues         []PMigrationIssue    `json:"issues,omitempty"`
}

func (p *PMigrationPlan) HasBlockers() bool {
	if p == nil {
		return true
	}
	for _, issue := range p.Issues {
		if issue.Severity == "error" {
			return true
		}
	}
	return false
}

type PMigrationBWSnapshot struct {
	Folders map[string]PMigrationBWFolder `json:"folders"`
}

type PMigrationBWFolder struct {
	Notes   []string `json:"notes,omitempty"`
	SSHKeys []string `json:"ssh_keys,omitempty"`
}

var nonPName = regexp.MustCompile(`[^a-z0-9]+`)

// NormalizeLegacyPName 把旧 project/bundle 名确定性地规范到小写 kebab-case。
func NormalizeLegacyPName(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = nonPName.ReplaceAllString(name, "-")
	return strings.Trim(name, "-")
}

func repositoryHasLegacyLayout(repoDir string) bool {
	for _, name := range []string{types.VaultProjectsDir, types.VaultBundlesDir} {
		entries, err := os.ReadDir(filepath.Join(repoDir, name))
		if err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}

// BuildPMigrationPlan 只读取给定仓库目录和 BW 元数据快照。
func BuildPMigrationPlan(repoDir string, bw PMigrationBWSnapshot) (*PMigrationPlan, error) {
	plan := &PMigrationPlan{Version: PMigrationJournalVersion, LegacyDetected: repositoryHasLegacyLayout(repoDir)}
	projects, err := readLegacyProjects(repoDir)
	if err != nil {
		return nil, err
	}
	bundles, err := readLegacyBundles(repoDir)
	if err != nil {
		return nil, err
	}
	if len(projects) > 0 || len(bundles) > 0 {
		plan.LegacyDetected = true
	}

	rawByNormalized := map[string][]string{}
	addName := func(kind, raw string) string {
		normalized := NormalizeLegacyPName(raw)
		if normalized == "" || !types.IsValidPName(normalized) {
			plan.Issues = append(plan.Issues, PMigrationIssue{
				Code: "invalid_name", Severity: "error", Source: kind + "/" + raw,
				Message: fmt.Sprintf("%q 无法规范为合法 P 名", raw),
			})
			return ""
		}
		rawByNormalized[normalized] = append(rawByNormalized[normalized], kind+"/"+raw)
		if normalized != raw {
			plan.Issues = append(plan.Issues, PMigrationIssue{
				Code: "name_normalized", Severity: "info", Source: raw, Target: normalized,
				Message: fmt.Sprintf("%q 将规范为 %q", raw, normalized),
			})
		}
		return normalized
	}

	bundleByRaw := make(map[string]legacyBundle)
	manifestByName := make(map[string]PMigrationManifest)
	for _, b := range bundles {
		name := addName("bundle", b.Name)
		b.Normalized = name
		bundleByRaw[b.Name] = b
		if name == "" {
			continue
		}
		manifestByName[name] = PMigrationManifest{Name: name, Title: b.Name, Description: b.Description}
		plane := string(b.Scope)
		for _, source := range b.AssetPaths {
			rel, _ := filepath.Rel(filepath.Join(repoDir, types.VaultBundlesDir, b.DirName), source)
			target := filepath.ToSlash(filepath.Join(name, "public", plane, rel))
			plan.GitMoves = append(plan.GitMoves, PMigrationGitMove{
				Source: filepath.ToSlash(filepath.Join(types.VaultBundlesDir, b.DirName, rel)),
				Target: target,
			})
		}
	}

	projectNames := make(map[string]string)
	for _, project := range projects {
		name := addName("project", project.FileName)
		projectNames[project.FileName] = name
		if name == "" {
			continue
		}
		manifest := manifestByName[name]
		manifest.Name = name
		manifest.Title = project.Name
		if manifest.Title == "" {
			manifest.Title = project.FileName
		}
		manifest.Description = firstNonEmpty(project.Description, manifest.Description)
		manifest.IDEs = append([]string(nil), project.IDEs...)
		manifest.Editor = project.Editor
		for _, ref := range project.Bundles {
			b, ok := bundleByRaw[ref]
			if !ok {
				plan.Issues = append(plan.Issues, PMigrationIssue{
					Code: "missing_reference", Severity: "error",
					Source: filepath.ToSlash(filepath.Join(types.VaultProjectsDir, project.FileName+".yaml")),
					Target: ref, Message: fmt.Sprintf("project %q 引用缺失 bundle %q", project.FileName, ref),
				})
				continue
			}
			if b.Normalized == name {
				continue
			}
			if b.Scope != types.BundleScopeProject {
				plan.Issues = append(plan.Issues, PMigrationIssue{
					Code: "invalid_user_reference", Severity: "error", Source: project.FileName, Target: ref,
					Message: fmt.Sprintf("project %q 引用了 user bundle %q，无法转为 requires", project.FileName, ref),
				})
				continue
			}
			manifest.Requires = append(manifest.Requires, b.Normalized)
		}
		manifest.Requires = uniqueSorted(manifest.Requires)
		manifestByName[name] = manifest
	}

	for normalized, sources := range rawByNormalized {
		sources = uniqueSorted(sources)
		rawNames := map[string]struct{}{}
		for _, source := range sources {
			_, raw, _ := strings.Cut(source, "/")
			rawNames[raw] = struct{}{}
		}
		if len(rawNames) > 1 {
			plan.Issues = append(plan.Issues, PMigrationIssue{
				Code: "case_normalization_collision", Severity: "error",
				Source: strings.Join(sources, ", "), Target: normalized,
				Message: fmt.Sprintf("多个旧名称规范到同一 P %q", normalized),
			})
		}
	}

	for name, manifest := range manifestByName {
		if existingPathExists(repoDir, filepath.ToSlash(filepath.Join(name, types.ProjectManifestFileName))) {
			plan.Issues = append(plan.Issues, PMigrationIssue{
				Code: "target_exists", Severity: "error", Target: name,
				Message: fmt.Sprintf("目标 P %q 已存在，拒绝覆盖", name),
			})
		}
		plan.Manifests = append(plan.Manifests, manifest)
	}

	for folder, contents := range bw.Folders {
		target, ok := legacyBWTarget(folder, projectNames, bundleByRaw)
		if !ok {
			clean := strings.Trim(strings.ReplaceAll(strings.TrimSpace(folder), "\\", "/"), "/")
			if strings.HasPrefix(strings.ToLower(clean), "bundle/") {
				plan.LegacyDetected = true
				plan.Issues = append(plan.Issues, PMigrationIssue{
					Code: "missing_bw_owner", Severity: "error", Source: folder,
					Message: fmt.Sprintf("Bitwarden folder %q 没有对应的旧 bundle 声明", folder),
				})
			}
			continue
		}
		plan.LegacyDetected = true
		for _, path := range contents.Notes {
			clean, pathErr := migrationLogicalPath(path)
			if pathErr != nil {
				plan.Issues = append(plan.Issues, PMigrationIssue{
					Code: "invalid_bw_path", Severity: "error", Source: folder + "/" + path,
					Message: pathErr.Error(),
				})
				continue
			}
			plan.BWMoves = append(plan.BWMoves, PMigrationBWMove{SourceFolder: folder, TargetFolder: target, Path: clean, Kind: "note"})
		}
		for _, path := range contents.SSHKeys {
			clean, pathErr := migrationLogicalPath(path)
			if pathErr != nil {
				plan.Issues = append(plan.Issues, PMigrationIssue{
					Code: "invalid_bw_path", Severity: "error", Source: folder + "/" + path,
					Message: pathErr.Error(),
				})
				continue
			}
			plan.BWMoves = append(plan.BWMoves, PMigrationBWMove{SourceFolder: folder, TargetFolder: target, Path: clean, Kind: "sshkey"})
		}
	}
	checkMigrationTargetConflicts(repoDir, bw, plan)
	sortMigrationPlan(plan)
	plan.Fingerprint = migrationFingerprint(plan)
	return plan, nil
}

type legacyProject struct {
	FileName    string
	Name        string
	Description string
	Bundles     []string
	IDEs        []string
	Editor      string
}

func readLegacyProjects(repoDir string) ([]legacyProject, error) {
	dir := filepath.Join(repoDir, types.VaultProjectsDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []legacyProject
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var value types.Project
		if err := yaml.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("解析旧 project %s: %w", entry.Name(), err)
		}
		fileName := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".yaml"), ".yml")
		out = append(out, legacyProject{FileName: fileName, Name: value.Name, Description: value.Description, Bundles: value.Bundles, IDEs: value.IDEs, Editor: value.Editor})
	}
	return out, nil
}

type legacyBundle struct {
	DirName     string
	Name        string
	Normalized  string
	Description string
	Scope       types.BundleScope
	AssetPaths  []string
}

func readLegacyBundles(repoDir string) ([]legacyBundle, error) {
	dir := filepath.Join(repoDir, types.VaultBundlesDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []legacyBundle
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		value, _, err := bundle.LoadBundle(filepath.Join(dir, entry.Name()), nil)
		if err != nil {
			return nil, err
		}
		if value.Name == "" {
			continue
		}
		item := legacyBundle{DirName: entry.Name(), Name: value.Name, Description: value.Description, Scope: value.Scope}
		err = filepath.WalkDir(filepath.Join(dir, entry.Name()), func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("旧 bundle %q 含符号链接 %s，拒绝迁移以避免读取仓库外内容", entry.Name(), path)
			}
			if d.IsDir() || d.Name() == types.BundleManifestFileName {
				return nil
			}
			item.AssetPaths = append(item.AssetPaths, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func legacyBWTarget(folder string, projects map[string]string, bundles map[string]legacyBundle) (string, bool) {
	clean := strings.Trim(strings.ReplaceAll(strings.TrimSpace(folder), "\\", "/"), "/")
	if strings.HasPrefix(strings.ToLower(clean), "bundle/") {
		raw := clean[len("bundle/"):]
		for oldName, b := range bundles {
			if strings.EqualFold(oldName, raw) && b.Normalized != "" {
				return b.Normalized + "/private/" + string(b.Scope), true
			}
		}
		return "", false
	}
	for raw, normalized := range projects {
		if strings.EqualFold(clean, raw) && normalized != "" {
			return normalized + "/private/project", true
		}
	}
	return "", false
}

func checkMigrationTargetConflicts(repoDir string, bw PMigrationBWSnapshot, plan *PMigrationPlan) {
	targets := map[string]string{}
	for _, move := range plan.GitMoves {
		key := strings.ToLower(move.Target)
		if prev, ok := targets[key]; ok && prev != move.Source {
			addTargetConflict(plan, prev, move.Source, move.Target)
		} else {
			targets[key] = move.Source
		}
		if existingPathExists(repoDir, move.Target) {
			addTargetConflict(plan, move.Source, "existing Git", move.Target)
		}
	}
	bwTargets := map[string]string{}
	for _, move := range plan.BWMoves {
		key := strings.ToLower(move.TargetFolder + "/" + move.Path)
		source := move.Kind + ":" + move.SourceFolder + "/" + move.Path
		if prev, ok := bwTargets[key]; ok && prev != source {
			addTargetConflict(plan, prev, source, move.TargetFolder+"/"+move.Path)
		} else {
			bwTargets[key] = source
		}
		if existing, ok := bw.Folders[move.TargetFolder]; ok {
			for _, path := range append(append([]string{}, existing.Notes...), existing.SSHKeys...) {
				if strings.EqualFold(cleanLogicalPath(path), move.Path) {
					addTargetConflict(plan, source, "existing Bitwarden", move.TargetFolder+"/"+move.Path)
				}
			}
		}
		// BW 目标映射到 P/private/<plane>/<path>，不得与 Git 已有或待迁文件重叠。
		logical := strings.ToLower(move.TargetFolder + "/" + move.Path)
		exactLogical := move.TargetFolder + "/" + move.Path
		if sourceGit, ok := targets[logical]; ok || existingPathExists(repoDir, exactLogical) {
			if !ok {
				sourceGit = "existing Git"
			}
			plan.Issues = append(plan.Issues, PMigrationIssue{
				Code: "git_bw_path_conflict", Severity: "error", Source: sourceGit + " ↔ " + source,
				Target:  move.TargetFolder + "/" + move.Path,
				Message: "Git 与 Bitwarden 将持有同一 P/plane/相对路径",
			})
		}
	}
}

func addTargetConflict(plan *PMigrationPlan, a, b, target string) {
	plan.Issues = append(plan.Issues, PMigrationIssue{
		Code: "target_conflict", Severity: "error", Source: a + " ↔ " + b, Target: target,
		Message: fmt.Sprintf("迁移目标 %q 存在冲突", target),
	})
}

func existingPathExists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

func cleanLogicalPath(path string) string {
	return strings.Trim(strings.ReplaceAll(filepath.ToSlash(filepath.Clean(path)), "\\", "/"), "/")
}

func migrationLogicalPath(raw string) (string, error) {
	clean, err := secrets.RemoteNoteName(secrets.SyncTarget{}, raw)
	if err != nil {
		return "", fmt.Errorf("Bitwarden 路径 %q 非法，拒绝迁移: %w", raw, err)
	}
	return clean, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortMigrationPlan(plan *PMigrationPlan) {
	sort.Slice(plan.Manifests, func(i, j int) bool { return plan.Manifests[i].Name < plan.Manifests[j].Name })
	sort.Slice(plan.GitMoves, func(i, j int) bool { return plan.GitMoves[i].Target < plan.GitMoves[j].Target })
	sort.Slice(plan.BWMoves, func(i, j int) bool {
		a, b := plan.BWMoves[i], plan.BWMoves[j]
		return a.TargetFolder+"/"+a.Path+"/"+a.Kind < b.TargetFolder+"/"+b.Path+"/"+b.Kind
	})
	sort.Slice(plan.Issues, func(i, j int) bool {
		a, b := plan.Issues[i], plan.Issues[j]
		return a.Severity+"/"+a.Code+"/"+a.Source < b.Severity+"/"+b.Code+"/"+b.Source
	})
}

func migrationFingerprint(plan *PMigrationPlan) string {
	copyPlan := *plan
	copyPlan.Fingerprint = ""
	data, _ := json.Marshal(copyPlan)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type PMigrationPhase string

const (
	PMigrationPending       PMigrationPhase = "pending"
	PMigrationBackedUp      PMigrationPhase = "local_backed_up"
	PMigrationGitPrepared   PMigrationPhase = "git_prepared"
	PMigrationBWPrepared    PMigrationPhase = "bw_prepared"
	PMigrationLocalSwitched PMigrationPhase = "local_switched"
	PMigrationBWDeleted     PMigrationPhase = "legacy_bw_deleted"
	PMigrationGitDeleted    PMigrationPhase = "legacy_git_deleted"
	PMigrationComplete      PMigrationPhase = "complete"
)

type PMigrationJournal struct {
	Version         int             `json:"version"`
	PlanFingerprint string          `json:"plan_fingerprint"`
	Plan            *PMigrationPlan `json:"plan,omitempty"`
	Phase           PMigrationPhase `json:"phase"`
	BackupPath      string          `json:"backup_path,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at"`
	LastError       string          `json:"last_error,omitempty"`
}

// PMigrationBackend 的每个方法都必须幂等；状态日志在每个成功步骤后原子落盘。
type PMigrationBackend interface {
	BackupLocal(context.Context, *PMigrationPlan) (string, error)
	PrepareGit(context.Context, *PMigrationPlan) error
	VerifyGit(context.Context, *PMigrationPlan) error
	WriteBitwarden(context.Context, *PMigrationPlan) error
	VerifyBitwarden(context.Context, *PMigrationPlan) error
	SwitchLocal(context.Context, *PMigrationPlan, string) error
	DeleteLegacyBitwarden(context.Context, *PMigrationPlan) error
	DeleteLegacyGit(context.Context, *PMigrationPlan) error
}

// ExecutePMigration 从日志中的下一阶段继续；失败时保留日志和已验证的新节点，不提前删旧节点。
func ExecutePMigration(ctx context.Context, plan *PMigrationPlan, journalPath string, backend PMigrationBackend, reporter Reporter) (*PMigrationJournal, error) {
	if plan == nil || !plan.LegacyDetected {
		return nil, fmt.Errorf("未检测到旧结构")
	}
	if plan.HasBlockers() {
		return nil, fmt.Errorf("迁移预览包含阻断问题，拒绝执行")
	}
	if strings.TrimSpace(plan.Fingerprint) == "" || migrationFingerprint(plan) != plan.Fingerprint {
		return nil, fmt.Errorf("迁移计划指纹无效，请重新 preview")
	}
	journal, err := loadPMigrationJournal(journalPath, plan.Fingerprint)
	if err != nil {
		return nil, err
	}
	if journal.Plan == nil {
		planCopy := *plan
		journal.Plan = &planCopy
	}
	reporter = defaultReporter(reporter)
	run := func(next PMigrationPhase, action func() error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := action(); err != nil {
			journal.LastError = err.Error()
			journal.UpdatedAt = time.Now()
			_ = savePMigrationJournal(journalPath, journal)
			return err
		}
		journal.Phase = next
		journal.LastError = ""
		journal.UpdatedAt = time.Now()
		if err := savePMigrationJournal(journalPath, journal); err != nil {
			return err
		}
		emit(reporter, EventInfo, "p.migrate", "迁移阶段完成："+string(next), nil)
		return nil
	}
	if journal.Phase == PMigrationPending {
		if err := run(PMigrationBackedUp, func() error {
			path, err := backend.BackupLocal(ctx, plan)
			if err == nil {
				journal.BackupPath = path
			}
			return err
		}); err != nil {
			return journal, err
		}
	}
	if journal.Phase == PMigrationBackedUp {
		if err := run(PMigrationGitPrepared, func() error {
			if err := backend.PrepareGit(ctx, plan); err != nil {
				return err
			}
			return backend.VerifyGit(ctx, plan)
		}); err != nil {
			return journal, err
		}
	}
	if journal.Phase == PMigrationGitPrepared {
		if err := run(PMigrationBWPrepared, func() error {
			if err := backend.WriteBitwarden(ctx, plan); err != nil {
				return err
			}
			return backend.VerifyBitwarden(ctx, plan)
		}); err != nil {
			return journal, err
		}
	}
	if journal.Phase == PMigrationBWPrepared {
		if err := run(PMigrationLocalSwitched, func() error {
			return backend.SwitchLocal(ctx, plan, journal.BackupPath)
		}); err != nil {
			return journal, err
		}
	}
	if journal.Phase == PMigrationLocalSwitched {
		if err := run(PMigrationBWDeleted, func() error { return backend.DeleteLegacyBitwarden(ctx, plan) }); err != nil {
			return journal, err
		}
	}
	if journal.Phase == PMigrationBWDeleted {
		if err := run(PMigrationGitDeleted, func() error { return backend.DeleteLegacyGit(ctx, plan) }); err != nil {
			return journal, err
		}
	}
	if journal.Phase == PMigrationGitDeleted {
		if err := run(PMigrationComplete, func() error { return nil }); err != nil {
			return journal, err
		}
	}
	return journal, nil
}

func loadPMigrationJournal(path, fingerprint string) (*PMigrationJournal, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &PMigrationJournal{Version: PMigrationJournalVersion, PlanFingerprint: fingerprint, Plan: nil, Phase: PMigrationPending}, nil
	}
	if err != nil {
		return nil, err
	}
	var journal PMigrationJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("解析迁移恢复日志失败: %w", err)
	}
	if journal.Version != PMigrationJournalVersion || journal.PlanFingerprint != fingerprint {
		return nil, fmt.Errorf("迁移恢复日志与当前 preview 不匹配")
	}
	return &journal, nil
}

func savePMigrationJournal(path string, journal *PMigrationJournal) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
