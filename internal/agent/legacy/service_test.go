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
