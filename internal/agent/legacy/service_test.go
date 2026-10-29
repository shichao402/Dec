package legacy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 执行只认页面回传的指纹，代理自己重扫；残留变了就拒绝，不按旧结果删（需求 3.2）。
func TestServiceExecuteRequiresMatchingFingerprint(t *testing.T) {
	state, root := archiveFixture(t)
	a := filepath.Join(root, ".decs/a")
	put(t, a, "a", 0o644)
	items := []Item{{Rule: RuleDecsHome, Action: ActionDelete, Path: filepath.Join(root, ".decs"), IsDir: true, Size: 1}}
	var logs []string
	svc := &Service{
		StateDir: state,
		ScanFn:   func() (*Report, error) { return &Report{Items: append([]Item(nil), items...)}, nil },
		Logf:     func(f string, args ...any) { logs = append(logs, f) },
		Procs:    &fakeProcs{},
		Now:      func() time.Time { return fixedNow },
		Git:      GitOptions{Disabled: true},
	}
	v, err := svc.Scan()
	if err != nil || v.Empty || v.Fingerprint == "" || v.Rules[RuleDecsHome].ID != RuleDecsHome {
		t.Fatalf("扫描结果不对：%+v %v", v, err)
	}

	if _, err := svc.Execute(""); err == nil {
		t.Fatal("没有指纹不应执行")
	}
	// 确认之后残留变了：多出一项。
	items = append(items, Item{Rule: RuleConsoleData, Action: ActionDelete, Path: filepath.Join(root, "x"), Size: 1})
	if _, err := svc.Execute(v.Fingerprint); !errors.Is(err, ErrStale) {
		t.Fatalf("残留变化后应拒绝执行，得到 %v", err)
	}
	if _, err := os.Stat(a); err != nil {
		t.Fatal("拒绝执行时不应删任何东西")
	}
	items = items[:1]
	res, err := svc.Execute(v.Fingerprint)
	if err != nil || res.Archive == nil {
		t.Fatalf("指纹一致应执行：%v", err)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("应已删除")
	}
	if !strings.Contains(strings.Join(logs, "\n"), "旧版残留清理 %s：%d 项") {
		t.Fatalf("执行应写日志：%v", logs)
	}

	list, _ := svc.Archives()
	if len(list) != 1 {
		t.Fatalf("应有一份归档：%+v", list)
	}
	av, err := svc.Archive(list[0].ID)
	if err != nil || len(av.Items) != 1 || av.Items[0].Path != items[0].Path {
		t.Fatalf("归档清单不对：%+v %v", av, err)
	}
	if _, err := svc.Archive("../x"); err == nil {
		t.Fatal("非法 id 应被拒")
	}
	if _, err := svc.Restore(list[0].ID, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(a); string(got) != "a" {
		t.Fatal("应已还原")
	}
	if err := svc.DeleteArchive(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.Archives(); len(list) != 0 {
		t.Fatal("归档应已删除")
	}
}

// 指纹只看会决定删什么的字段：顺序无关，只列出的凭据不影响，大小或条目变了就变。
func TestFingerprint(t *testing.T) {
	a := Item{Rule: "H01", Action: ActionDelete, Path: "/a", Size: 1}
	b := Item{Rule: "H10", Action: ActionStrip, Path: "/b", Entries: []string{"x", "y"}, Size: 2}
	l := Item{Rule: "H13", Action: ActionList, Path: "/k"}
	base := Fingerprint(&Report{Items: []Item{a, b}})
	if Fingerprint(&Report{Items: []Item{b, a, l}}) != base {
		t.Fatal("顺序与只列出项不应影响指纹")
	}
	b2 := b
	b2.Entries = []string{"y", "x"}
	if Fingerprint(&Report{Items: []Item{a, b2}}) != base {
		t.Fatal("条目顺序不应影响指纹")
	}
	for _, mut := range []func(*Item){
		func(i *Item) { i.Size = 3 },
		func(i *Item) { i.Entries = []string{"x"} },
		func(i *Item) { i.Path = "/c" },
	} {
		c := b
		c.Entries = append([]string(nil), b.Entries...)
		mut(&c)
		if Fingerprint(&Report{Items: []Item{a, c}}) == base {
			t.Fatalf("删除内容变了，指纹应改变：%+v", c)
		}
	}
}

// TestExecuteRejectsConcurrent 锁住「执行中直接拒绝」而不是排队。
//
// 2026-10-29 用户反馈清理时重复点击。后端原本用 mu.Lock() 排队：
// 第二次不会并发破坏数据，但会静静等到第一次结束后照样执行，
// 用的是它自己重扫的结果，于是同一批文件被处理两遍，界面还只报一句
// 「残留与上次扫描不一致」，看不出发生了什么。清理会删文件，宁可明确拒绝。
func TestExecuteRejectsConcurrent(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	item := filepath.Join(root, ".dec", "cache")
	put(t, filepath.Join(item, "x"), "x", 0o644)

	started := make(chan struct{})
	release := make(chan struct{})
	s := &Service{
		StateDir: state,
		ScanFn: func() (*Report, error) {
			return &Report{Items: []Item{{Rule: RuleDecHome, Action: ActionDelete, Path: item, IsDir: true}}}, nil
		},
		Procs: &fakeProcs{},
		Now:   func() time.Time { return fixedNow },
	}
	// 用 ScanFn 里的钩子卡住第一次执行，制造「正在清理」的状态。
	s.ScanFn = func() (*Report, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return &Report{Items: []Item{{Rule: RuleDecHome, Action: ActionDelete, Path: item, IsDir: true}}}, nil
	}

	fp := Fingerprint(&Report{Items: []Item{{Rule: RuleDecHome, Action: ActionDelete, Path: item, IsDir: true}}})
	done := make(chan error, 1)
	go func() { _, err := s.Execute(fp); done <- err }()
	<-started

	// 第一次还在跑：第二次必须立刻被拒，而不是挂在那里等。
	_, err := s.Execute(fp)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("执行中的重复请求应返回 ErrBusy，实际: %v", err)
	}
	// 流式版本同样要拒绝。
	_, err = s.ExecuteWithProgress(fp, func(Progress) {})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("执行中的重复流式请求应返回 ErrBusy，实际: %v", err)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("第一次执行不应失败: %v", err)
	}
	// 锁必须真的释放了，否则之后再也清理不了。
	if _, err := s.Execute(fp); errors.Is(err, ErrBusy) {
		t.Fatal("第一次执行结束后锁没释放，后续清理会被永久拒绝")
	}
}

// TestExecuteWithProgressForwards 保证带进度的执行把帧传出去。
func TestExecuteWithProgressForwards(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	item := filepath.Join(root, ".dec", "cache")
	put(t, filepath.Join(item, "x"), "x", 0o644)
	rep := &Report{Items: []Item{{Rule: RuleDecHome, Action: ActionDelete, Path: item, IsDir: true}}}
	s := &Service{StateDir: state, ScanFn: func() (*Report, error) { return rep, nil },
		Procs: &fakeProcs{}, Now: func() time.Time { return fixedNow }}

	var steps []string
	res, err := s.ExecuteWithProgress(Fingerprint(rep), func(p Progress) { steps = append(steps, p.Step) })
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if res.Empty {
		t.Fatal("不应为空")
	}
	if len(steps) == 0 {
		t.Fatal("没有收到任何进度帧")
	}
	if steps[0] != StepArchive {
		t.Errorf("第一帧应是归档，实际 %q", steps[0])
	}
}
