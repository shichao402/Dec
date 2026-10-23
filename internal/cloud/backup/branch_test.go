package backup

// 分支跟随行为的用例。
//
// 起因（2026-10-23 生产故障）：SSOT 与审计仓库由 go-git 的 PlainInit 初始化，
// 它的默认分支是 master（go-git 不读 git 的 init.defaultBranch 配置）。
// 推送代码原先固定用 "main"，fetch 的 refspec 指向一个不存在的分支，
// push 随之报 "src refspec main does not match any"。
//
// 这类错误在日志里只剩一句「推送失败」，与分支名毫无关联，排查代价很高。
// 所以这里用本地仓库（不碰网络）把「跟随 HEAD」钉住。

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// initRepoWithCommit 建一个只有一个提交的本地仓库，返回仓库与其分支名。
func initRepoWithCommit(t *testing.T) (*git.Repository, string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("初始化仓库失败: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("取工作树失败: %v", err)
	}
	file := filepath.Join(dir, "README.md")
	if err := os.WriteFile(file, []byte("x\n"), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("暂存失败: %v", err)
	}
	sig := &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("取 HEAD 失败: %v", err)
	}
	return repo, head.Name().Short()
}

// TestBranchFollowsLocalHead 确认推送用的分支名来自本地 HEAD，
// 而不是写死的 main。
func TestBranchFollowsLocalHead(t *testing.T) {
	repo, branch := initRepoWithCommit(t)
	p := &Pusher{Target: Target{Name: "冷备"}}

	if got := p.branchOf(repo); got != branch {
		t.Errorf("推送分支应为本地 HEAD 的 %q，实际 %q；"+
			"写死 main 会让 PlainInit 建出的 master 仓库推不上去", branch, got)
	}
}

// TestBranchFallsBackWhenNoHead 确认空仓库不会 panic，退回默认值。
func TestBranchFallsBackWhenNoHead(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("初始化仓库失败: %v", err)
	}
	p := &Pusher{Target: Target{Name: "冷备"}}
	got := p.branchOf(repo)
	if got == "" {
		t.Error("空仓库也要给出一个可用的分支名，不能是空串")
	}
}

// TestBranchExplicitTargetWins 确认显式配置的 Target.Branch 优先。
//
// 这是给「远端用 main、本地是 master」这类特例留的口子，
// 也让分支名在配置层面可见。
func TestBranchExplicitTargetWins(t *testing.T) {
	p := &Pusher{Target: Target{Name: "冷备", Branch: "trunk"}}
	if got := p.branch(); got != "trunk" {
		t.Errorf("显式配置的分支应被采用，实际 %q", got)
	}
}
