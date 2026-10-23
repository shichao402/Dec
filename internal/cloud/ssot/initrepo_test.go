package ssot

import (
	"os"
	"path/filepath"
	"testing"
)

// 为什么要有这个测试：
//
// 2026-10-23 生产故障——SSOT 与审计仓库都是 go-git 的 PlainInit 建出来的，
// 初始分支是 master；而远端 GitHub 仓库的默认分支是 main。推送代码当时写死
// 推 main，fetch 的 refspec 指向一个本地不存在的分支，push 报
// "src refspec main does not match any"；改成跟随本地 HEAD 之后能推上去了，
// 但推到的是 refs/heads/master，与远端默认分支 main 分叉，远端页面看到的
// 仍是一条孤立的初始化提交。
//
// 根子在初始化：go-git 不读 git 的 init.defaultBranch，PlainInit 的初始分支
// 永远是 master。所以这里直接断言 InitRepo 建出的是 main。
func TestInitRepoUsesMain(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")

	repo, err := InitRepo(dir)
	if err != nil {
		t.Fatalf("InitRepo: %v", err)
	}

	head, err := repo.Head()
	if err != nil {
		// 空仓库此时 HEAD 指向一个尚不存在的分支，返回的是符号引用而非错误，
		// 但不同版本行为有差异，这里只关心分支名，取不到就退一步读 HEAD 引用。
		ref, refErr := repo.Reference("HEAD", false)
		if refErr != nil {
			t.Fatalf("读取 HEAD 失败: err=%v refErr=%v", err, refErr)
		}
		if got := ref.Target().Short(); got != "main" {
			t.Fatalf("初始分支应为 main，实际 %q", got)
		}
		return
	}

	if !head.Name().IsBranch() {
		t.Fatalf("HEAD 不是分支引用: %s", head.Name())
	}
	if got := head.Name().Short(); got != "main" {
		t.Fatalf("初始分支应为 main，实际 %q；"+
			"改回 PlainInit 会让本地 master 与远端 main 分叉，冷备推不上去", got)
	}
}

// 已有仓库不能被 InitRepo 覆盖：它只负责首次初始化。
func TestInitRepoRejectsExisting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	if _, err := InitRepo(dir); err != nil {
		t.Fatalf("首次 InitRepo: %v", err)
	}
	if _, err := InitRepo(dir); err == nil {
		t.Fatal("对已存在的仓库应返回错误，否则会覆盖既有历史")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf(".git 目录应存在: %v", err)
	}
}
