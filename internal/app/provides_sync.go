package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/sysproc"
	"github.com/shichao402/Dec/internal/types"
)

type ProjectProvidesState struct {
	ProjectName string
	ConfigPath  string
	Worktree    string
	// ProvidesRoot 是作者目录基准点（相对项目根，新项目默认 DecAssets）。
	ProvidesRoot string
	// AuthorDirs 是当前基准点下四类资产的实际目录，供 Console 直接展示。
	AuthorDirs []string
	Provides   map[string]types.ProjectProvide
	Targets    map[string]string
}

type SaveProjectProvidesInput struct {
	ProjectRoot  string
	ProvidesRoot string
	Provides     map[string]types.ProjectProvide
}

type ProvideSyncMode string

const (
	ProvideSyncAuto ProvideSyncMode = "auto"
	ProvideSyncPull ProvideSyncMode = "pull"
	ProvideSyncPush ProvideSyncMode = "push"
)

type ProvideSyncEntry struct {
	Key              string
	Source           string
	Target           string
	Type             string
	Status           string
	Action           string
	LocalMtime       string
	RemoteCommitTime string
	LastSyncTime     string
	LocalContent     string
	RemoteContent    string
	Conflict         bool
}

type ProvidesSyncResult struct {
	Mode            ProvideSyncMode
	Worktree        string
	Branch          string
	RemoteBranch    string
	Ahead           int
	Behind          int
	MergeInProgress bool
	ConflictedPaths []string
	Dirty           bool
	Entries         []ProvideSyncEntry
	Changed         int
	Backfilled      int
	Committed       bool
	Pushed          bool
	Commit          string
	HasConflicts    bool
	SecretsPulled   int
	SecretsPushed   int
	SecretsPending  bool
}

func LoadProjectProvides(projectRoot string) (*ProjectProvidesState, error) {
	mgr := config.NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	state := &ProjectProvidesState{
		ProjectName:  cfg.ProjectName,
		ConfigPath:   filepath.Join(mgr.GetDecDir(), "config.yaml"),
		Worktree:     filepath.Join(mgr.GetDecDir(), "sync", "vault"),
		ProvidesRoot: cfg.ProvidesRoot,
		AuthorDirs:   authorDirs(cfg.ProvidesRoot),
		Provides:     cloneProvides(cfg.Provides),
		Targets:      map[string]string{},
	}
	for key, item := range cfg.Provides {
		target, err := config.ProjectProvideTarget(cfg.ProjectName, item)
		if err != nil {
			return nil, err
		}
		state.Targets[key] = target
	}
	return state, nil
}

func SaveProjectProvides(input SaveProjectProvidesInput) (*ProjectProvidesState, error) {
	mgr := config.NewProjectConfigManager(input.ProjectRoot)
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	cfg.ProvidesRoot = input.ProvidesRoot
	cfg.Provides = cloneProvides(input.Provides)
	if err := mgr.SaveProjectConfig(cfg); err != nil {
		return nil, err
	}
	return LoadProjectProvides(input.ProjectRoot)
}

// authorDirs 列出当前基准点下的四个作者目录，Console 用它告诉人该把资产放哪。
func authorDirs(providesRoot string) []string {
	out := make([]string, 0, len(bundle.VaultAssetKinds))
	for _, kind := range bundle.VaultAssetKinds {
		out = append(out, config.ProvideAuthorDir(providesRoot, kind.Dir))
	}
	return out
}

func cloneProvides(in map[string]types.ProjectProvide) map[string]types.ProjectProvide {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]types.ProjectProvide, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func PreviewProjectProvidesSync(ctx context.Context, projectRoot string, reporter Reporter) (*ProvidesSyncResult, error) {
	return syncProjectProvides(ctx, projectRoot, ProvideSyncAuto, "", false, reporter)
}

func SyncProjectProvides(ctx context.Context, projectRoot string, mode ProvideSyncMode, reporter Reporter) (*ProvidesSyncResult, error) {
	return SyncProjectProvidesAction(ctx, projectRoot, mode, "", reporter)
}

func SyncProjectProvidesAction(ctx context.Context, projectRoot string, mode ProvideSyncMode, conflictAction string, reporter Reporter) (*ProvidesSyncResult, error) {
	if mode == "" {
		mode = ProvideSyncAuto
	}
	if mode != ProvideSyncAuto && mode != ProvideSyncPull && mode != ProvideSyncPush {
		return nil, fmt.Errorf("provides sync mode %q 必须是 auto、pull 或 push", mode)
	}
	if conflictAction != "" && conflictAction != "continue" && conflictAction != "abort" {
		return nil, fmt.Errorf("conflict action %q 必须是 continue 或 abort", conflictAction)
	}
	return syncProjectProvides(ctx, projectRoot, mode, conflictAction, true, reporter)
}

func syncProjectProvides(ctx context.Context, projectRoot string, mode ProvideSyncMode, conflictAction string, execute bool, reporter Reporter) (*ProvidesSyncResult, error) {
	reporter = defaultReporter(reporter)
	state, err := LoadProjectProvides(projectRoot)
	if err != nil {
		return nil, err
	}
	if len(state.Provides) == 0 {
		return &ProvidesSyncResult{Mode: mode, Worktree: state.Worktree}, nil
	}
	result := &ProvidesSyncResult{Mode: mode, Worktree: state.Worktree}
	if execute && mode == ProvideSyncPush {
		return nil, fmt.Errorf("官方 provides 禁止本机推柜；请用提供方 CI 的 dec-registry publish-provides")
	}
	err = repo.WithPersistentWorktree(state.Worktree, projectRoot, func(worktree, branch, defaultBranch string) error {
		result.Branch, result.RemoteBranch = branch, defaultBranch
		conflicts, merge, err := gitConflictState(ctx, worktree)
		if err != nil {
			return err
		}
		result.ConflictedPaths, result.MergeInProgress = conflicts, merge
		if conflictAction == "abort" {
			if merge {
				if err := runGit(ctx, worktree, "merge", "--abort"); err != nil {
					return err
				}
			}
			result.ConflictedPaths = nil
			result.MergeInProgress = false
			return nil
		}
		if conflictAction == "continue" {
			if !merge {
				return fmt.Errorf("当前没有待继续的 Git merge")
			}
			if len(conflicts) > 0 {
				result.HasConflicts = true
				return nil
			}
			if err := runGit(ctx, worktree, "add", "--all"); err != nil {
				return err
			}
			if err := runGit(ctx, worktree, "commit", "--no-edit"); err != nil {
				return err
			}
			if result.Backfilled, err = backfillProvideSources(projectRoot, worktree, state.ProjectName, state.Provides); err != nil {
				return err
			}
			if err := runGit(ctx, worktree, "push", "origin", "HEAD:"+defaultBranch); err != nil {
				return fmt.Errorf("冲突已解决但 push 失败，可重试继续: %w", err)
			}
			result.Pushed = true
			result.Commit, _ = gitOutput(ctx, worktree, "rev-parse", "HEAD")
			return nil
		}
		if merge || len(conflicts) > 0 {
			result.HasConflicts = true
			return nil
		}
		remoteRef := "refs/remotes/origin/" + defaultBranch
		if err := runGit(ctx, worktree, "fetch", "--prune", "origin",
			"+refs/heads/"+defaultBranch+":"+remoteRef); err != nil {
			return err
		}
		result.Ahead, result.Behind, err = gitAheadBehind(ctx, worktree, remoteRef)
		if err != nil {
			return err
		}
		if !execute {
			result.Entries, err = planProvideSync(projectRoot, worktree, state.ProjectName, state.Provides, mode)
			return err
		}
		if mode == ProvideSyncPush && result.Behind > 0 {
			return fmt.Errorf("远端领先 %d 个提交；仅 Push 拒绝覆盖，请改用自动同步或 Pull", result.Behind)
		}

		// 先把作者源提交成真正的 Git 本地提交，再与 FETCH_HEAD 做三方 merge。
		// 未提交文件直接 merge 会被 Git 拒绝，也无法得到正确 merge-base。
		if mode == ProvideSyncAuto || mode == ProvideSyncPush {
			result.Changed, err = importProvideSources(projectRoot, worktree, state.ProjectName, state.Provides)
			if err != nil {
				return err
			}
			if result.Changed > 0 {
				if err := runGit(ctx, worktree, "add", "--all"); err != nil {
					return err
				}
				if err := runGit(ctx, worktree, "commit", "-m", "provides: import "+state.ProjectName); err != nil {
					return err
				}
				result.Committed = true
			}
		}
		result.Ahead, result.Behind, err = gitAheadBehind(ctx, worktree, remoteRef)
		if err != nil {
			return err
		}
		if result.Behind > 0 {
			if result.Ahead == 0 {
				err = runGit(ctx, worktree, "merge", "--ff-only", remoteRef)
			} else {
				err = runGit(ctx, worktree, "merge", "--no-edit", remoteRef)
			}
			if err != nil {
				mergeErr := err
				result.ConflictedPaths, result.MergeInProgress, _ = gitConflictState(ctx, worktree)
				if result.MergeInProgress || len(result.ConflictedPaths) > 0 {
					result.HasConflicts = true
					return nil // 保留 Git merge 状态与冲突文件。
				}
				return mergeErr
			}
			result.Ahead, result.Behind, err = gitAheadBehind(ctx, worktree, remoteRef)
			if err != nil {
				return err
			}
		}
		entries, err := planProvideSync(projectRoot, worktree, state.ProjectName, state.Provides, mode)
		if err != nil {
			return err
		}
		result.Entries = entries
		if mode == ProvideSyncAuto || mode == ProvideSyncPull {
			result.Backfilled, err = backfillProvideSources(projectRoot, worktree, state.ProjectName, state.Provides)
			if err != nil {
				return err
			}
		}
		result.Commit, _ = gitOutput(ctx, worktree, "rev-parse", "HEAD")
		result.Ahead, result.Behind, err = gitAheadBehind(ctx, worktree, remoteRef)
		if err != nil {
			return err
		}
		if mode == ProvideSyncPull || result.Ahead == 0 {
			return nil
		}
		// 官方快照只由 CI 写入 Dec registry，本机 auto 同步不再 push 私仓。
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Secrets 不由 provides 声明：`.secrets/<project>` 整树的归属由 SyncTarget
	// 规则唯一决定，所以这里按平面无条件带上，而不是看登记了什么。
	if execute && !result.HasConflicts {
		workspace := NewWorkspace(WorkspaceProject, projectRoot)
		switch mode {
		case ProvideSyncPull:
			summary, pullErr := pullEnabledSecretsBundlesForWorkspace(ctx, workspace, []string{state.ProjectName}, reporter)
			if pullErr != nil {
				return nil, pullErr
			}
			if summary != nil {
				result.SecretsPulled = summary.NoteCount + summary.SSHKeyCount
			}
		case ProvideSyncPush:
			summary, pushErr := PushWorkspaceSecretsBundles(ctx, workspace, reporter)
			if pushErr != nil {
				return nil, pushErr
			}
			if summary != nil {
				result.SecretsPushed = summary.CreatedCount + summary.UpdatedCount
			}
		default:
			// Secret 正文没有三方合并，自动模式先 pull 再 push 会静默吃掉本地改动。
			// 公开资产照常自动同步，secret 留给用户选方向。
			result.SecretsPending = projectHasSecrets(projectRoot, state.ProjectName)
		}
	}
	if result.HasConflicts {
		emit(reporter, EventWarn, "provides.sync", "provides 同步存在冲突，已保留现场", nil)
	} else if execute {
		emit(reporter, EventInfo, "provides.sync", fmt.Sprintf("provides 同步完成：Git 更新 %d，作者回填 %d", result.Changed, result.Backfilled), nil)
	}
	return result, nil
}

func projectHasSecrets(projectRoot, projectName string) bool {
	if strings.TrimSpace(projectName) == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(projectRoot, secrets.SecretsRootDir, projectName))
	return err == nil && info.IsDir()
}

func planProvideSync(projectRoot, worktree, projectName string, provides map[string]types.ProjectProvide, mode ProvideSyncMode) ([]ProvideSyncEntry, error) {
	keys := make([]string, 0, len(provides))
	for key := range provides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ProvideSyncEntry, 0, len(keys))
	for _, key := range keys {
		item := provides[key]
		targetRel, _ := config.ProjectProvideTarget(projectName, item)
		entry := ProvideSyncEntry{Key: key, Source: item.Source, Target: targetRel, Type: item.Type}
		source := filepath.Join(projectRoot, filepath.FromSlash(item.Source))
		sourceExists, err := safeProvideSource(projectRoot, source, item)
		if err != nil {
			return nil, fmt.Errorf("provides.%s: %w", key, err)
		}
		if sourceExists {
			entry.LocalMtime = provideModTime(source)
		}
		target := filepath.Join(worktree, filepath.FromSlash(targetRel))
		targetExists, err := safeTargetExists(worktree, target)
		if err != nil {
			return nil, fmt.Errorf("provides.%s target: %w", key, err)
		}
		if targetExists {
			entry.RemoteCommitTime, _ = gitOutput(context.Background(), worktree, "log", "-1", "--format=%cI", "--", targetRel)
			entry.LastSyncTime = entry.RemoteCommitTime
		}
		if sourceExists {
			entry.LocalContent = providePreviewText(source)
		}
		if targetExists {
			entry.RemoteContent = providePreviewText(target)
		}
		switch {
		case sourceExists && targetExists:
			equal, err := sameProvideContent(source, target)
			if err != nil {
				return nil, err
			}
			if equal {
				entry.Status = "in_sync"
			} else if mode == ProvideSyncPush {
				entry.Status, entry.Action = "modified", "author_to_vault"
			} else {
				entry.Status, entry.Conflict = "modified", true
			}
		case sourceExists:
			if mode == ProvideSyncPull {
				entry.Status, entry.Conflict = "vault_missing", true
			} else {
				entry.Status, entry.Action = "author_only", "author_to_vault"
			}
		case targetExists:
			if mode == ProvideSyncPush {
				entry.Status, entry.Conflict = "author_missing", true
			} else {
				entry.Status, entry.Action = "vault_only", "vault_to_author"
			}
		default:
			entry.Status, entry.Conflict = "missing", true
		}
		out = append(out, entry)
	}
	return out, nil
}

func importProvideSources(projectRoot, worktree, projectName string, provides map[string]types.ProjectProvide) (int, error) {
	changed := 0
	for key, item := range provides {
		targetRel, err := config.ProjectProvideTarget(projectName, item)
		if err != nil {
			return changed, err
		}
		source := filepath.Join(projectRoot, filepath.FromSlash(item.Source))
		target := filepath.Join(worktree, filepath.FromSlash(targetRel))
		exists, err := safeProvideSource(projectRoot, source, item)
		if err != nil {
			return changed, fmt.Errorf("provides.%s: %w", key, err)
		}
		if !exists {
			continue
		}
		if targetExists, err := safeTargetExists(worktree, target); err != nil {
			return changed, err
		} else if targetExists {
			equal, err := sameProvideContent(source, target)
			if err != nil {
				return changed, err
			}
			if equal {
				continue
			}
		}
		if err := replaceProvidePath(source, target); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func backfillProvideSources(projectRoot, worktree, projectName string, provides map[string]types.ProjectProvide) (int, error) {
	changed := 0
	for key, item := range provides {
		targetRel, err := config.ProjectProvideTarget(projectName, item)
		if err != nil {
			return changed, err
		}
		source := filepath.Join(projectRoot, filepath.FromSlash(item.Source))
		target := filepath.Join(worktree, filepath.FromSlash(targetRel))
		exists, err := safeTargetExists(worktree, target)
		if err != nil {
			return changed, fmt.Errorf("provides.%s: %w", key, err)
		}
		if !exists {
			continue
		}
		if sourceExists, _ := safeProvideSource(projectRoot, source, item); sourceExists {
			equal, err := sameProvideContent(source, target)
			if err != nil {
				return changed, err
			}
			if equal {
				continue
			}
		}
		if err := replaceProvidePath(target, source); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func safeProvideSource(root, source string, item types.ProjectProvide) (bool, error) {
	rel, err := filepath.Rel(root, source)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("source 逃逸 project root")
	}
	info, err := os.Lstat(source)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("source 不允许符号链接")
	}
	if item.Type == "skill" || item.Type == "command" {
		if !info.IsDir() {
			return false, fmt.Errorf("%s source 必须是目录", item.Type)
		}
	} else if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s source 必须是普通文件", item.Type)
	}
	if info.IsDir() {
		if err := rejectSymlinks(source); err != nil {
			return false, err
		}
	}
	return true, nil
}

func safeTargetExists(root, target string) (bool, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("target 逃逸 worktree")
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("target 不允许符号链接")
	}
	if info.IsDir() {
		if err := rejectSymlinks(target); err != nil {
			return false, err
		}
	}
	return true, nil
}

func rejectSymlinks(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("不允许符号链接 %s", path)
		}
		return nil
	})
}

func provideModTime(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339)
}

func providePreviewText(root string) string {
	const limit = 128 * 1024
	var out strings.Builder
	appendFile := func(path, label string) {
		if out.Len() >= limit {
			return
		}
		file, err := os.Open(path)
		if err != nil {
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, int64(limit-out.Len())))
		if err != nil {
			return
		}
		if strings.IndexByte(string(data), 0) >= 0 {
			data = []byte("（二进制文件）")
		}
		if label != "" {
			fmt.Fprintf(&out, "===== %s =====\n", label)
		}
		out.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			out.WriteByte('\n')
		}
	}
	info, err := os.Stat(root)
	if err != nil {
		return ""
	}
	if !info.IsDir() {
		appendFile(root, "")
		return out.String()
	}
	_ = filepath.Walk(root, func(current string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.Mode().IsRegular() || out.Len() >= limit {
			return nil
		}
		rel, _ := filepath.Rel(root, current)
		appendFile(current, filepath.ToSlash(rel))
		return nil
	})
	if out.Len() >= limit {
		out.WriteString("\n（预览已截断）\n")
	}
	return out.String()
}

func replaceProvidePath(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(source, target)
	}
	return copyFile(source, target)
}

func sameProvideContent(a, b string) (bool, error) {
	ah, err := hashProvidePath(a)
	if err != nil {
		return false, err
	}
	bh, err := hashProvidePath(b)
	return err == nil && ah == bh, err
}

func hashProvidePath(root string) (string, error) {
	h := sha256.New()
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("不允许符号链接 %s", root)
	}
	if info.Mode().IsRegular() {
		f, err := os.Open(root)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("不允许符号链接 %s", path)
		}
		rel, _ := filepath.Rel(root, path)
		_, _ = io.WriteString(h, filepath.ToSlash(rel)+"\x00")
		if entry.Type().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(h, f)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

func runGit(ctx context.Context, dir string, args ...string) error {
	_, err := gitOutput(ctx, dir, args...)
	return err
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := sysproc.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func gitAheadBehind(ctx context.Context, dir, remoteRef string) (int, int, error) {
	out, err := gitOutput(ctx, dir, "rev-list", "--left-right", "--count", "HEAD..."+remoteRef)
	if err != nil {
		return 0, 0, err
	}
	var ahead, behind int
	if _, err := fmt.Sscanf(out, "%d %d", &ahead, &behind); err != nil {
		return 0, 0, fmt.Errorf("解析 Git 分叉状态失败: %q", out)
	}
	return ahead, behind, nil
}

func gitConflictState(ctx context.Context, dir string) ([]string, bool, error) {
	out, err := gitOutput(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, false, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			paths = append(paths, strings.TrimSpace(line))
		}
	}
	gitPath, err := gitOutput(ctx, dir, "rev-parse", "--git-path", "MERGE_HEAD")
	if err != nil {
		return nil, false, err
	}
	if !filepath.IsAbs(gitPath) {
		gitPath = filepath.Join(dir, gitPath)
	}
	_, statErr := os.Stat(gitPath)
	return paths, statErr == nil, nil
}

func gitCachedChanged(ctx context.Context, dir string) (bool, error) {
	cmd := sysproc.CommandContext(ctx, "git", "-C", dir, "diff", "--cached", "--quiet")
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}

func gitDirty(ctx context.Context, dir string) (bool, error) {
	out, err := gitOutput(ctx, dir, "status", "--porcelain")
	return strings.TrimSpace(out) != "", err
}
