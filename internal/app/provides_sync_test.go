package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/sysproc"
	"github.com/shichao402/Dec/internal/types"
)

func TestProjectProvidesSyncImportsAndKeepsSecretsOutOfGit(t *testing.T) {
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	seed := filepath.Join(base, "seed")
	runProvidesGit(t, "", "init", "--bare", "--initial-branch=main", remote)
	runProvidesGit(t, "", "init", "--initial-branch=main", seed)
	runProvidesGit(t, seed, "config", "user.name", "test")
	runProvidesGit(t, seed, "config", "user.email", "test@example.com")
	if err := os.MkdirAll(filepath.Join(seed, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "demo", "dec.yaml"), []byte("name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runProvidesGit(t, seed, "add", ".")
	runProvidesGit(t, seed, "commit", "-m", "seed")
	runProvidesGit(t, seed, "remote", "add", "origin", remote)
	runProvidesGit(t, seed, "push", "-u", "origin", "main")

	decHome := filepath.Join(base, "dec-home")
	t.Setenv("DEC_HOME", decHome)
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	bare, err := repo.GetBareRepoDir()
	if err != nil {
		t.Fatal(err)
	}
	runProvidesGit(t, "", "--git-dir", bare, "config", "user.name", "test")
	runProvidesGit(t, "", "--git-dir", bare, "config", "user.email", "test@example.com")

	project := filepath.Join(base, "author")
	if err := os.MkdirAll(filepath.Join(project, "skills", "demo-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "skills", "demo-skill", "SKILL.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Secrets 不经 provides，但仍必须证明它们不会被 Git 同步带走。
	const secretBody = "TOP_SECRET_MUST_NOT_ENTER_GIT"
	if err := os.MkdirAll(filepath.Join(project, ".secrets", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".secrets", "demo", "private.env"), []byte(secretBody), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &types.ProjectConfig{
		ProjectName: "demo",
		Provides: map[string]types.ProjectProvide{
			"skill": {
				Source: "skills/demo-skill", Visibility: types.AssetVisibilityPublic,
				Plane: types.AssetPlaneLocal, Type: "skill", Name: "demo-skill",
			},
		},
	}
	if err := config.NewProjectConfigManager(project).SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewProjectProvidesSync(context.Background(), project, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.HasConflicts || len(preview.Entries) != 1 {
		t.Fatalf("preview = %#v", preview)
	}
	result, err := SyncProjectProvides(context.Background(), project, ProvideSyncAuto, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Committed || !result.Pushed || result.Changed != 1 || !result.SecretsPending {
		t.Fatalf("sync = %#v", result)
	}

	skill := runProvidesGit(t, "", "--git-dir", remote, "show", "main:demo/public/local/skills/demo-skill/SKILL.md")
	if skill != "# demo" {
		t.Fatalf("skill body = %q", skill)
	}
	cmd := sysproc.Command("git", "--git-dir", remote, "grep", secretBody, "main")
	if err := cmd.Run(); err == nil {
		t.Fatal("secret 正文进入了 Git")
	}
	cmd = sysproc.Command("git", "--git-dir", remote, "ls-tree", "-r", "--name-only", "main")
	tree, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(tree), ".secrets") || strings.Contains(string(tree), "private/local/secrets") {
		t.Fatalf("secrets 不应出现在 Git 树里:\n%s", tree)
	}

	again, err := PreviewProjectProvidesSync(context.Background(), project, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range again.Entries {
		if entry.Status != "in_sync" {
			t.Fatalf("二次 preview entry = %#v", entry)
		}
	}

	// 制造持久 worktree 与远端的真实文本冲突；同步必须保留 MERGE_HEAD 与冲突文件，
	// 不能自行拼接正文或自动 abort。
	localSkill := filepath.Join(project, "skills", "demo-skill", "SKILL.md")
	if err := os.WriteFile(localSkill, []byte("# local\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runProvidesGit(t, seed, "pull", "--ff-only")
	remoteSkill := filepath.Join(seed, "demo", "public", "local", "skills", "demo-skill", "SKILL.md")
	if err := os.WriteFile(remoteSkill, []byte("# remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runProvidesGit(t, seed, "add", ".")
	runProvidesGit(t, seed, "commit", "-m", "remote conflict")
	runProvidesGit(t, seed, "push", "origin", "main")

	conflicted, err := SyncProjectProvides(context.Background(), project, ProvideSyncAuto, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !conflicted.HasConflicts || !conflicted.MergeInProgress || len(conflicted.ConflictedPaths) == 0 {
		t.Fatalf("Git 冲突现场未保留: %#v", conflicted)
	}
	mergeHead := runProvidesGit(t, result.Worktree, "rev-parse", "--git-path", "MERGE_HEAD")
	if !filepath.IsAbs(mergeHead) {
		mergeHead = filepath.Join(result.Worktree, mergeHead)
	}
	if _, err := os.Stat(mergeHead); err != nil {
		t.Fatalf("MERGE_HEAD 不存在: %v", err)
	}

	if err := os.WriteFile(filepath.Join(result.Worktree, "demo", "public", "local", "skills", "demo-skill", "SKILL.md"), []byte("# resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runProvidesGit(t, result.Worktree, "add", ".")
	continued, err := SyncProjectProvidesAction(context.Background(), project, ProvideSyncAuto, "continue", nil)
	if err != nil {
		t.Fatal(err)
	}
	if continued.HasConflicts || !continued.Pushed {
		t.Fatalf("continue = %#v", continued)
	}
	if got, err := os.ReadFile(localSkill); err != nil || string(got) != "# resolved\n" {
		t.Fatalf("解决结果未回填作者源: %q, %v", got, err)
	}
}

func TestPlanProvideSyncPreservesModifiedAsAutoConflict(t *testing.T) {
	project := t.TempDir()
	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "rules", "rule.mdc"), []byte("author"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(worktree, "demo", "public", "local", "rules", "rule.mdc")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("vault"), 0o644); err != nil {
		t.Fatal(err)
	}
	provides := map[string]types.ProjectProvide{
		"rule": {
			Source: "rules/rule.mdc", Visibility: types.AssetVisibilityPublic,
			Plane: types.AssetPlaneLocal, Type: "rule", Name: "rule",
		},
	}
	entries, err := planProvideSync(project, worktree, "demo", provides, ProvideSyncAuto)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].Conflict || entries[0].Action != "" {
		t.Fatalf("auto 不应覆盖双方已有差异: %#v", entries)
	}
}

func runProvidesGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := sysproc.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
