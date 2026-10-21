package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// 冷备与审计推送的核心不变量：
//
//  1. 远端是本地历史的祖先时才推。
//  2. 远端被改写过就停，绝不 force-push。
//  3. 部署期间暂停，恢复后立刻补推。
//
// 这些测试用本地 bare 仓库模拟远端，不依赖网络。

// remoteFixture 是本地 bare 仓库充当的远端。
type remoteFixture struct {
	dir  string
	repo *git.Repository
}

// newRemote 创建一个 bare 仓库。
func newRemote(t *testing.T) *remoteFixture {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, true)
	if err != nil {
		t.Fatalf("初始化 bare 仓库失败: %v", err)
	}
	return &remoteFixture{dir: dir, repo: repo}
}

// workFixture 是被推送的本地仓库。
type workFixture struct {
	dir  string
	repo *git.Repository
}

func newWork(t *testing.T) *workFixture {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("初始化工作仓库失败: %v", err)
	}
	return &workFixture{dir: dir, repo: repo}
}

// commit 写一个文件并提交。
func (w *workFixture) commit(t *testing.T, name, content string) plumbing.Hash {
	t.Helper()
	full := filepath.Join(w.dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	wt, err := w.repo.Worktree()
	if err != nil {
		t.Fatalf("读取工作树失败: %v", err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatalf("暂存失败: %v", err)
	}
	hash, err := wt.Commit("c-"+name, &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@dec.local", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	return hash
}

// EnsureRemote 给工作仓库配一个 origin。
func (w *workFixture) EnsureRemote(url string) error {
	if _, err := w.repo.Remote("origin"); err == nil {
		if err := w.repo.DeleteRemote("origin"); err != nil {
			return err
		}
	}
	_, err := w.repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})
	return err
}

// headOf 返回工作仓库当前 head。
func (w *workFixture) headOf(t *testing.T) string {
	t.Helper()
	ref, err := w.repo.Head()
	if err != nil {
		t.Fatalf("读取 head 失败: %v", err)
	}
	return ref.Hash().String()
}

// pusher 构造一个指向本地 bare 仓库的 Pusher。
//
// URL 用 file:// 形式，这样不走 SSH，也不需要 deploy key。
func pusher(t *testing.T, remote *remoteFixture) *Pusher {
	t.Helper()
	return &Pusher{Target: Target{
		Name:   "冷备",
		URL:    remote.dir,
		Branch: "master",
	}}
}

// TestPushFirstTime 验证首次推送成功且远端 head 前进。
func TestPushFirstTime(t *testing.T) {
	r := newRemote(t)
	w := newWork(t)
	w.commit(t, "a.txt", "one")

	p := pusher(t, r)
	if err := p.EnsureRemote(w.repo); err != nil {
		t.Fatalf("配置远端失败: %v", err)
	}
	out, err := p.Push(context.Background(), w.repo)
	if err != nil {
		t.Fatalf("推送失败: %v", err)
	}
	if !out.Pushed {
		t.Fatal("首次推送应产生推送")
	}

	ref, err := r.repo.Reference(plumbing.NewBranchReferenceName("master"), false)
	if err != nil {
		t.Fatalf("远端没有 master 分支: %v", err)
	}
	if ref.Hash().String() != out.Head {
		t.Fatalf("远端 head %s 与推送结果 %s 不一致", ref.Hash(), out.Head)
	}
}

// TestPushNoChange 验证已是最新时不重复推送。
func TestPushNoChange(t *testing.T) {
	r := newRemote(t)
	w := newWork(t)
	w.commit(t, "a.txt", "one")

	p := pusher(t, r)
	if err := p.EnsureRemote(w.repo); err != nil {
		t.Fatalf("配置远端失败: %v", err)
	}
	if _, err := p.Push(context.Background(), w.repo); err != nil {
		t.Fatalf("首次推送失败: %v", err)
	}
	out, err := p.Push(context.Background(), w.repo)
	if err != nil {
		t.Fatalf("第二次推送失败: %v", err)
	}
	if out.Pushed {
		t.Fatal("无变更时不应推送")
	}
}

// TestPushIncremental 验证增量推送。
func TestPushIncremental(t *testing.T) {
	r := newRemote(t)
	w := newWork(t)
	w.commit(t, "a.txt", "one")

	p := pusher(t, r)
	if err := p.EnsureRemote(w.repo); err != nil {
		t.Fatalf("配置远端失败: %v", err)
	}
	if _, err := p.Push(context.Background(), w.repo); err != nil {
		t.Fatalf("首次推送失败: %v", err)
	}

	w.commit(t, "b.txt", "two")
	out, err := p.Push(context.Background(), w.repo)
	if err != nil {
		t.Fatalf("增量推送失败: %v", err)
	}
	if !out.Pushed {
		t.Fatal("有新 commit 时应推送")
	}
}

// TestPushRejectsRewrittenRemote 验证远端历史被改写时停止推送（需求 15.2）。
//
// 场景：远端被换成一条与本地无关的历史（两个独立仓库各自提交）。
// 这时绝不能覆盖远端——那会把“有人动过”的证据抹掉。
func TestPushRejectsRewrittenRemote(t *testing.T) {
	r := newRemote(t)

	// 先用另一个仓库把远端推到一份独立历史，模拟“远端被人改过”。
	other := newWork(t)
	other.commit(t, "remote-only.txt", "from elsewhere")
	if err := other.EnsureRemote(r.dir); err != nil {
		t.Fatalf("配置另一个仓库的远端失败: %v", err)
	}
	if err := other.repo.Push(&git.PushOptions{
		RefSpecs: []config.RefSpec{"refs/heads/master:refs/heads/master"},
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		t.Fatalf("另一个仓库推送失败: %v", err)
	}

	// 本地仓库是另一条完全独立的历史。
	w := newWork(t)
	w.commit(t, "a.txt", "local-only")
	p := pusher(t, r)
	if err := p.EnsureRemote(w.repo); err != nil {
		t.Fatalf("配置远端失败: %v", err)
	}

	_, err := p.Push(context.Background(), w.repo)
	if err == nil {
		t.Fatal("远端历史无关时应拒绝推送")
	}
	if !errors.Is(err, ErrRemoteRewritten) {
		t.Fatalf("应返回 ErrRemoteRewritten，实际: %v", err)
	}

	// 远端必须保持原样，没有被本地覆盖。
	ref, err := r.repo.Reference(plumbing.NewBranchReferenceName("master"), false)
	if err != nil {
		t.Fatalf("读取远端引用失败: %v", err)
	}
	if ref.Hash().String() != other.headOf(t) {
		t.Fatalf("远端被覆盖了: %s", ref.Hash())
	}
}
