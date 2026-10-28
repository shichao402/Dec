package legacy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// 执行后整份还原：整删对象回到原位，内容、权限、修改时间、软链接都和原来一致；
// 共享文件原路径仍在，写到旁边（需求 6.3）。
func TestRestoreWholeArchive(t *testing.T) {
	state, root := archiveFixture(t)
	dir := filepath.Join(root, ".decs")
	f := put(t, filepath.Join(dir, "bin", "dec"), "binary", 0o751)
	mt := fixedNow.Add(-48 * time.Hour)
	_ = os.Chtimes(f, mt, mt)
	if err := os.Symlink("bin/dec", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "bin"), 0o750); err != nil {
		t.Fatal(err)
	}
	rc := put(t, filepath.Join(root, ".bashrc"), "export A=1\nexport PATH=~/.dec/bin:$PATH\n", 0o644)

	res, err := Execute(state, &Report{Items: []Item{
		{Rule: RuleDecsHome, Action: ActionDelete, Path: dir, IsDir: true},
		{Rule: RuleShellRC, Action: ActionStrip, Path: rc, Project: ""},
	}}, execOpts(&fakeProcs{}))
	if err != nil || res.Failed() {
		t.Fatalf("%v %+v", err, res)
	}

	later := fixedNow.Add(time.Hour)
	rr, err := Restore(state, res.Archive.ID, nil, later)
	if err != nil {
		t.Fatal(err)
	}
	if rr.Failed() || len(rr.Items) != 2 {
		t.Fatalf("还原应全部成功：%+v", rr.Items)
	}

	got, err := os.ReadFile(f)
	if err != nil || string(got) != "binary" {
		t.Fatalf("文件没有还原：%v %q", err, got)
	}
	info, _ := os.Stat(f)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o751 {
		t.Fatalf("权限应为 751，实际 %o", info.Mode().Perm())
	}
	if !info.ModTime().Equal(mt) {
		t.Fatalf("修改时间应还原为 %s，实际 %s", mt, info.ModTime())
	}
	if di, _ := os.Stat(filepath.Join(dir, "bin")); runtime.GOOS != "windows" && di.Mode().Perm() != 0o750 {
		t.Fatalf("目录权限应为 750，实际 %o", di.Mode().Perm())
	}
	if l, err := os.Readlink(filepath.Join(dir, "link")); err != nil || l != "bin/dec" {
		t.Fatalf("软链接没有原样还原：%v %q", err, l)
	}

	var rcRes RestoreItem
	for _, it := range rr.Items {
		if it.Item.Path == rc {
			rcRes = it
		}
	}
	want := rc + ".restored-20261028T103000Z"
	if !rcRes.Renamed || rcRes.Dest != want {
		t.Fatalf("共享文件原路径已存在，应写到 %s：%+v", want, rcRes)
	}
	if got, _ := os.ReadFile(want); !strings.Contains(string(got), ".dec/bin") {
		t.Fatalf("还原出的应是删除前的原文：%q", got)
	}
	if got, _ := os.ReadFile(rc); strings.Contains(string(got), ".dec/bin") {
		t.Fatal("当前文件不应被覆盖")
	}
	assertNoStaging(t, root)
}

// 单项还原只还原选中的那一项；重名时依次加序号，不覆盖任何东西。
func TestRestoreSingleItemNoOverwrite(t *testing.T) {
	state, root := archiveFixture(t)
	a := put(t, filepath.Join(root, ".decs"), "a", 0o644)
	b := put(t, filepath.Join(root, ".claude-internal"), "b", 0o644)
	res, err := Execute(state, &Report{Items: []Item{
		{Rule: RuleDecsHome, Action: ActionDelete, Path: a},
		{Rule: RuleInternalHomes, Action: ActionDelete, Path: b},
	}}, execOpts(&fakeProcs{}))
	if err != nil || res.Failed() {
		t.Fatalf("%v %+v", err, res)
	}
	idx := -1
	for i, it := range res.Archive.Manifest.Items {
		if it.Path == b {
			idx = i
		}
	}

	// 原路径被新文件占了，备用名也被占了。
	put(t, b, "new", 0o644)
	put(t, b+".restored-20261028T093000Z", "older restore", 0o644)
	rr, err := Restore(state, res.Archive.ID, []int{idx}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Items) != 1 || !rr.Items[0].OK {
		t.Fatalf("应只还原一项且成功：%+v", rr.Items)
	}
	if rr.Items[0].Dest != b+".restored-20261028T093000Z-2" {
		t.Fatalf("应加序号避开已占用的备用名：%s", rr.Items[0].Dest)
	}
	for p, want := range map[string]string{b: "new", b + ".restored-20261028T093000Z": "older restore", rr.Items[0].Dest: "b"} {
		if got, _ := os.ReadFile(p); string(got) != want {
			t.Fatalf("%s 应为 %q，实际 %q", p, want, got)
		}
	}
	if _, err := os.Stat(a); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("未选中的项不应被还原")
	}
	if _, err := Restore(state, res.Archive.ID, []int{99}, fixedNow); err == nil {
		t.Fatal("越界下标应报错")
	}
}

// 项目里的命中还原后提示自行提交（需求 8.7）。
func TestRestoreProjectNote(t *testing.T) {
	state, root := archiveFixture(t)
	p := put(t, filepath.Join(root, "proj/.dec/config.yaml"), "x", 0o644)
	res, err := Execute(state, &Report{Items: []Item{{Rule: RuleProjectDec, Action: ActionDelete, Path: p, Project: filepath.Join(root, "proj")}}}, execOpts(&fakeProcs{}))
	if err != nil || res.Failed() {
		t.Fatalf("%v %+v", err, res)
	}
	rr, err := Restore(state, res.Archive.ID, nil, fixedNow)
	if err != nil || rr.Failed() {
		t.Fatalf("%v %+v", err, rr)
	}
	if !strings.Contains(strings.Join(rr.Items[0].Notes, ""), "自行提交") {
		t.Fatalf("应提示自行提交：%+v", rr.Items[0].Notes)
	}
}

// 归档损坏或清单被改成指向别处时拒绝还原，不写任何文件。
func TestRestoreRejectsTampered(t *testing.T) {
	state, root := archiveFixture(t)
	p := put(t, filepath.Join(root, ".decs/a"), "a", 0o644)
	a, err := WriteArchive(state, []Item{{Rule: RuleDecsHome, Action: ActionDelete, Path: filepath.Join(root, ".decs")}}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(filepath.Join(root, ".decs"))

	// 清单把条目改到命中之外：sidecar 与归档内 manifest 不一致，校验先拦下。
	sidecar := filepath.Join(ArchiveDir(state), a.ID+".json")
	orig, _ := os.ReadFile(sidecar)
	evil := strings.Replace(string(orig), `"path": "`+p+`"`, `"path": "`+filepath.Join(root, "evil")+`"`, 1)
	_ = os.WriteFile(sidecar, []byte(evil), 0o600)
	if _, err := Restore(state, a.ID, nil, fixedNow); err == nil {
		t.Fatal("被改过的清单应拒绝还原")
	}
	_ = os.WriteFile(sidecar, orig, 0o600)

	// 清单路径检查本身：即使校验通过，条目越出命中也拒绝。
	m, _ := LoadManifest(state, a.ID)
	m.Entries[len(m.Entries)-1].Path = filepath.Join(root, "evil")
	if err := checkManifestPaths(m); err == nil {
		t.Fatal("越出命中的条目应被拒绝")
	}

	// 归档本体损坏。
	tgz := filepath.Join(ArchiveDir(state), a.ID+".tar.gz")
	data, _ := os.ReadFile(tgz)
	data[len(data)/2] ^= 0xff
	_ = os.WriteFile(tgz, data, 0o600)
	if _, err := Restore(state, a.ID, nil, fixedNow); err == nil {
		t.Fatal("损坏的归档应拒绝还原")
	}
	for _, x := range []string{filepath.Join(root, ".decs"), filepath.Join(root, "evil")} {
		if _, err := os.Lstat(x); err == nil {
			t.Fatalf("拒绝还原时不应写出 %s", x)
		}
	}
	assertNoStaging(t, root)

	if _, err := Restore(state, "../../etc", nil, fixedNow); err == nil {
		t.Fatal("非法 id 应拒绝")
	}
}

func assertNoStaging(t *testing.T, root string) {
	t.Helper()
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(d.Name(), ".dec-restore-") {
			t.Fatalf("留下了临时目录 %s", p)
		}
		return nil
	})
}

func TestListAndDeleteArchives(t *testing.T) {
	state, root := archiveFixture(t)
	if l, err := ListArchives(state); err != nil || len(l) != 0 {
		t.Fatalf("没有归档目录时应返回空列表：%v %v", l, err)
	}
	put(t, filepath.Join(root, ".decs"), "aaaa", 0o644)
	a1, err := WriteArchive(state, []Item{{Rule: RuleDecsHome, Action: ActionDelete, Path: filepath.Join(root, ".decs")}}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := WriteArchive(state, []Item{{Rule: RuleDecsHome, Action: ActionDelete, Path: filepath.Join(root, ".decs")}}, fixedNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(ArchiveDir(state), a1.ID+".json"))

	l, err := ListArchives(state)
	if err != nil || len(l) != 2 {
		t.Fatalf("%v %+v", err, l)
	}
	if l[0].ID != a2.ID || l[0].Items != 1 || l[0].Bytes != 4 || l[0].CompressedSize == 0 || l[0].Problem != "" {
		t.Fatalf("新的在前且信息完整：%+v", l[0])
	}
	if l[1].ID != a1.ID || l[1].Problem == "" || !l[1].ExpiresAt.Equal(fixedNow.AddDate(0, 0, 365)) {
		t.Fatalf("缺清单的归档应标出问题，到期日按 id 推算：%+v", l[1])
	}

	if err := DeleteArchive(state, a2.ID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteArchive(state, a2.ID); err == nil {
		t.Fatal("删除不存在的归档应报错")
	}
	if err := DeleteArchive(state, "../x"); err == nil {
		t.Fatal("非法 id 应拒绝")
	}
}

// 到期才清；清单坏了按 id 时间判断；陌生文件不碰；陈旧半成品清掉（需求 6.1）。
func TestPurgeExpired(t *testing.T) {
	state, root := archiveFixture(t)
	put(t, filepath.Join(root, ".decs"), "x", 0o644)
	item := []Item{{Rule: RuleDecsHome, Action: ActionDelete, Path: filepath.Join(root, ".decs")}}
	old, err := WriteArchive(state, item, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	broken, err := WriteArchive(state, item, fixedNow.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(ArchiveDir(state), broken.ID+".json"), []byte("{"), 0o600)
	fresh, err := WriteArchive(state, item, fixedNow.AddDate(0, 6, 0))
	if err != nil {
		t.Fatal(err)
	}
	dir := ArchiveDir(state)
	stranger := put(t, filepath.Join(dir, "notes.txt"), "keep", 0o600)
	stale := put(t, filepath.Join(dir, "20261028T000000Z.tar.gz.partial"), "half", 0o600)
	_ = os.Chtimes(stale, fixedNow, fixedNow)

	// 第 364 天：都不到期。
	if p, err := PurgeExpired(state, fixedNow.AddDate(0, 0, 364)); err != nil || len(p) != 0 {
		t.Fatalf("未到期不应清除：%v %v", p, err)
	}
	// 第 365 天：两份旧的到期，新的保留。
	p, err := PurgeExpired(state, fixedNow.AddDate(0, 0, 365).Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p, ",") != broken.ID+","+old.ID {
		t.Fatalf("应清除两份旧归档，实际 %v", p)
	}
	for _, x := range []string{old.ID + ".tar.gz", old.ID + ".json", broken.ID + ".tar.gz", broken.ID + ".json"} {
		if _, err := os.Stat(filepath.Join(dir, x)); err == nil {
			t.Fatalf("%s 应已清除", x)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, fresh.ID+".tar.gz")); err != nil {
		t.Fatal("未到期的归档不应清除")
	}
	if _, err := os.Stat(stranger); err != nil {
		t.Fatal("不认识的文件不应碰")
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("陈旧半成品应清除")
	}
}

func TestRunRetentionRunsAtStartup(t *testing.T) {
	state, root := archiveFixture(t)
	put(t, filepath.Join(root, ".decs"), "x", 0o644)
	a, err := WriteArchive(state, []Item{{Rule: RuleDecsHome, Action: ActionDelete, Path: filepath.Join(root, ".decs")}}, time.Now().AddDate(-2, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var logs []string
	done := make(chan struct{})
	go func() {
		RunRetention(ctx, state, time.Hour, func(f string, args ...any) {
			mu.Lock()
			logs = append(logs, f)
			mu.Unlock()
		})
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(ArchiveDir(state), a.ID+".tar.gz")); errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("启动时应立即清除一次")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 1 || !strings.Contains(logs[0], "保留期") {
		t.Fatalf("清除应写日志：%v", logs)
	}
}
