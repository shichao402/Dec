package backup

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/shichao402/Dec/internal/cloud/ssot"
)

// TestPushContinuesAfterSsotRetention 锁住 ADR 0061 的前提：
// 本机 SSOT 按保留期回收成 shallow 之后，冷备推送（go-git，fetch + 祖先校验 +
// 非 force push）仍然照常工作，且冷备里保留完整历史。
//
// 这条不成立的话，回收就等于把冷备永久卡死在"远端历史被改写"告警上。
func TestPushContinuesAfterSsotRetention(t *testing.T) {
	if !gitAvailable() {
		t.Skip("需要系统 git")
	}
	remote := newRemote(t)
	store, err := ssot.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	write := func(content string) string {
		rev, err := store.Update(ssot.Author{Name: "dev-1"}, func(tx *ssot.Tx) (string, error) {
			return "写入 " + content, tx.WriteFile("assets/demo/skill/a.md", []byte(content), 0o600)
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		return rev
	}
	first := write("a")
	write("b")
	write("c")

	p := &Pusher{Target: Target{Name: "冷备", URL: remote.dir}}
	open := func() *git.Repository {
		repo, err := git.PlainOpen(store.Dir())
		if err != nil {
			t.Fatalf("PlainOpen: %v", err)
		}
		return repo
	}
	if _, err := p.Push(context.Background(), open()); err != nil {
		t.Fatalf("首次推送: %v", err)
	}
	// 再推一次让 fetch 刷新跟踪引用：回收只认冷备里确定已有的提交。
	if _, err := p.Push(context.Background(), open()); err != nil {
		t.Fatalf("二次推送: %v", err)
	}

	// 把"现在"放到一年后：全部历史都超出保留期，边界落在 HEAD。
	res, err := store.GcAt(time.Now().Add(365*24*time.Hour), 0)
	if err != nil {
		t.Fatalf("GcAt: %v", err)
	}
	if !res.Truncated {
		t.Fatalf("冷备已覆盖时应截断: %+v", res)
	}
	if store.HasCommit(first) {
		t.Fatal("本机应已回收保留期之前的提交")
	}

	last := write("d")
	out, err := p.Push(context.Background(), open())
	if err != nil {
		t.Fatalf("回收后推送失败（冷备会被卡死）: %v", err)
	}
	if !out.Pushed || out.Head != last {
		t.Fatalf("回收后应推上新提交: %+v", out)
	}

	ref, err := remote.repo.Reference(plumbing.NewBranchReferenceName("main"), true)
	if err != nil || ref.Hash().String() != last {
		t.Fatalf("冷备 main = %v %v，期望 %s", ref, err, last)
	}
	iter, err := remote.repo.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	n := 0
	_ = iter.ForEach(func(*object.Commit) error { n++; return nil })
	// init + a + b + c + d
	if n != 5 {
		t.Fatalf("冷备应保留完整历史，实际 %d 个提交", n)
	}
	if _, err := remote.repo.CommitObject(plumbing.NewHash(first)); err != nil {
		t.Fatalf("冷备里应还能取到最早的提交: %v", err)
	}
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
