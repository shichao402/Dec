package ssot

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/sysproc"
)

// gcFixture 造一段跨越保留期的历史：
//
//	init(T0) → A(T0+1d) → B(T0+100d) → C(T0+200d) → D(T0+300d)
//
// 以 now = T0+400d 回收时，保留期起点是 T0+220d，边界应落在 C。
type gcFixture struct {
	s          *Store
	t0         time.Time
	a, b, c, d string
	now        time.Time
}

func newGcFixture(t *testing.T) *gcFixture {
	t.Helper()
	requireGit(t)
	s := newTestStore(t)
	f := &gcFixture{s: s, t0: time.Now()}
	day := 24 * time.Hour
	write := func(offset time.Duration, content string) string {
		at := f.t0.Add(offset)
		s.clock = func() time.Time { return at }
		rev, err := s.Update(Author{Name: "dev-1", Source: "mcp"}, func(tx *Tx) (string, error) {
			return "写入 " + content, tx.WriteFile("assets/demo/skill/a.md", []byte(content), 0o600)
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		return rev
	}
	f.a = write(1*day, "a")
	f.b = write(100*day, "b")
	f.c = write(200*day, "c")
	f.d = write(300*day, "d")
	s.clock = time.Now
	f.now = f.t0.Add(400 * day)
	return f
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("需要系统 git")
	}
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := sysproc.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// archive 建一个裸仓库当冷备，把 rev 推到它的 main。
func (f *gcFixture) archive(t *testing.T, rev string) string {
	t.Helper()
	bare := t.TempDir()
	gitRun(t, bare, "init", "-q", "--bare", "-b", "main")
	gitRun(t, f.s.Dir(), "remote", "add", "origin", bare)
	gitRun(t, f.s.Dir(), "push", "-q", "origin", rev+":refs/heads/main")
	gitRun(t, f.s.Dir(), "fetch", "-q", "origin")
	return bare
}

func TestGcKeepsEverythingWithoutArchive(t *testing.T) {
	f := newGcFixture(t)
	res, err := f.s.GcAt(f.now, 0)
	if err != nil {
		t.Fatalf("GcAt: %v", err)
	}
	if res.Truncated || res.Skipped == "" {
		t.Fatalf("没有冷备时不应截断，且要说明原因: %+v", res)
	}
	if !f.s.HasCommit(f.a) {
		t.Fatal("没有冷备时历史不得丢失")
	}
}

func TestGcTruncatesArchivedHistory(t *testing.T) {
	f := newGcFixture(t)
	bare := f.archive(t, f.d)
	head := f.s.Head()

	res, err := f.s.GcAt(f.now, 0)
	if err != nil {
		t.Fatalf("GcAt: %v", err)
	}
	if !res.Truncated || res.Baseline != f.c {
		t.Fatalf("应截断到 C=%s: %+v", f.c, res)
	}
	if f.s.Head() != head {
		t.Fatal("回收不得改变 HEAD（不是改写历史）")
	}

	hist, err := f.s.History(context.Background(), 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 2 || hist[0].Hash != f.d || hist[1].Hash != f.c || !hist[1].Baseline {
		t.Fatalf("保留的历史应是 [D, C(起点)]: %+v", hist)
	}
	for _, gone := range []string{f.a, f.b} {
		if f.s.HasCommit(gone) {
			t.Fatalf("保留期之前的提交 %s 应已回收", gone)
		}
	}
	if data, err := f.s.FileAt(f.c, "assets/demo/skill/a.md"); err != nil || string(data) != "c" {
		t.Fatalf("起点快照应完整可读: %q %v", data, err)
	}
	paths, err := f.s.CommitPaths(f.c)
	if err != nil || len(paths) == 0 {
		t.Fatalf("起点提交应视为整棵树: %v %v", paths, err)
	}
	since, shallow, err := f.s.RetainedSince()
	if err != nil || !shallow || !since.Equal(f.t0.Add(200*24*time.Hour).UTC().Truncate(time.Second)) {
		t.Fatalf("RetainedSince = %v %v %v", since, shallow, err)
	}

	// 回收后仍能正常写入，并以非 force 方式继续推到冷备。
	at := f.t0.Add(350 * 24 * time.Hour)
	f.s.clock = func() time.Time { return at }
	rev, err := f.s.Update(Author{Name: "dev-1"}, func(tx *Tx) (string, error) {
		return "回收后写入", tx.WriteFile("assets/demo/skill/a.md", []byte("e"), 0o600)
	})
	if err != nil {
		t.Fatalf("回收后写入失败: %v", err)
	}
	gitRun(t, f.s.Dir(), "push", "-q", "origin", "main")
	if got := gitRun(t, bare, "rev-parse", "main"); got != rev {
		t.Fatalf("冷备 head = %s，期望 %s", got, rev)
	}
	// 冷备仍是完整链：init + A..D + 新提交 = 6。
	if n, _ := strconv.Atoi(gitRun(t, bare, "rev-list", "--count", "main")); n != 6 {
		t.Fatalf("冷备应保留完整历史，实际 %d 个提交", n)
	}
	if got := gitRun(t, bare, "cat-file", "-p", f.a+":assets/demo/skill/a.md"); got != "a" {
		t.Fatalf("冷备里应还能取到最早的内容: %q", got)
	}

	// 幂等：再跑一次不会再截断。
	again, err := f.s.GcAt(f.now, 0)
	if err != nil || again.Truncated {
		t.Fatalf("重复回收不应再截断: %+v %v", again, err)
	}
}

func TestGcWaitsUntilArchiveCoversBoundary(t *testing.T) {
	f := newGcFixture(t)
	// 冷备只推到 B，边界 C 还不在冷备里。
	f.archive(t, f.b)
	res, err := f.s.GcAt(f.now, 0)
	if err != nil {
		t.Fatalf("GcAt: %v", err)
	}
	if res.Truncated || !strings.Contains(res.Skipped, "冷备") {
		t.Fatalf("冷备没覆盖边界时不得截断: %+v", res)
	}
	if !f.s.HasCommit(f.a) {
		t.Fatal("历史不得丢失")
	}
}

func TestGcNoopWhenAllWithinRetention(t *testing.T) {
	f := newGcFixture(t)
	f.archive(t, f.d)
	// now = T0+90d：保留期起点在 T0 之前，全部历史都在保留期内。
	res, err := f.s.GcAt(f.t0.Add(HistoryRetention/2), 1<<30)
	if err != nil {
		t.Fatalf("GcAt: %v", err)
	}
	if res.Truncated || res.Skipped != "" || res.Packed {
		t.Fatalf("全部在保留期内时什么都不该做: %+v", res)
	}
}
