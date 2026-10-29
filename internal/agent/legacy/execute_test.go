package legacy

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"

	"github.com/shichao402/Dec/internal/sysproc"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fakeProcs struct {
	calls []string
	err   error
}

func (f *fakeProcs) StopWithin(root string) ([]string, error) {
	f.calls = append(f.calls, root)
	if f.err != nil {
		return nil, f.err
	}
	return []string{"42 " + root}, nil
}

func execOpts(p Processes) ExecOptions {
	return ExecOptions{Now: func() time.Time { return fixedNow }, Procs: p}
}

func resultFor(t *testing.T, r *Result, path string) ItemResult {
	t.Helper()
	for _, it := range r.Items {
		if it.Item.Path == path {
			return it
		}
	}
	t.Fatalf("结果里没有 %s：%+v", path, r.Items)
	return ItemResult{}
}

// 端到端：先归档后删除；整删、共享文件只删条目、只列出项不动，归档可校验（需求 4.2、5.1）。
func TestExecuteDeletesAfterArchive(t *testing.T) {
	state, root := archiveFixture(t)
	decDir := filepath.Join(root, ".dec", "cache")
	put(t, filepath.Join(decDir, "x"), "x", 0o644)
	rcReal := put(t, filepath.Join(root, "dotfiles/zshrc"), "export A=1\r\n# Dec\r\nexport PATH=\"$HOME/.dec/bin:$PATH\"\r\nalias l=ls\r\n", 0o640)
	rc := filepath.Join(root, ".zshrc")
	if err := os.Symlink(rcReal, rc); err != nil {
		t.Fatal(err)
	}
	mcp := put(t, filepath.Join(root, ".cursor/mcp.json"), `{
  "theme": "dark",
  "mcpServers": {
    "dec": {"url": "old"},
    "mine": {"command": "x"},
    "dec-old": {"url": "old2"},
    "dec_vault": {"url": "new"}
  }
}
`, 0o600)
	key := put(t, filepath.Join(root, ".ssh/dec_key"), "KEY", 0o600)

	report := &Report{Items: []Item{
		{Rule: RuleDecHome, Action: ActionDelete, Path: decDir, IsDir: true},
		{Rule: RuleShellRC, Action: ActionStrip, Path: rc, Entries: []string{"stale line numbers"}},
		{Rule: RuleUserMCP, Action: ActionStrip, Path: mcp, Entries: []string{"dec", "dec-old"}},
		{Rule: RuleSSH, Action: ActionList, Path: key},
	}}
	procs := &fakeProcs{}
	res, err := Execute(state, report, execOpts(procs))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed() {
		t.Fatalf("不应有失败项：%+v", res.Items)
	}
	if _, err := VerifyArchive(res.Archive.Path); err != nil {
		t.Fatalf("执行后归档应仍可校验：%v", err)
	}

	if r := resultFor(t, res, decDir); r.Status != StatusDeleted || len(r.Stopped) != 1 {
		t.Fatalf("H01 应先结束进程再删除：%+v", r)
	}
	if _, err := os.Lstat(decDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("整删对象应已删除")
	}

	// shell 配置：改的是链接指向的文件，链接保留；CRLF 与权限原样。
	if info, err := os.Lstat(rc); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("~/.zshrc 应仍是软链接")
	}
	got, _ := os.ReadFile(rcReal)
	if string(got) != "export A=1\r\nalias l=ls\r\n" {
		t.Fatalf("shell 配置改写不对：%q", got)
	}
	if info, _ := os.Stat(rcReal); runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("应保留原权限，实际 %o", info.Mode().Perm())
	}
	if r := resultFor(t, res, rc); len(r.Removed) != 2 {
		t.Fatalf("应按当前内容认定 2 行，实际 %+v", r.Removed)
	}

	// MCP：只删旧键，其余字节不变。
	got, _ = os.ReadFile(mcp)
	want := `{
  "theme": "dark",
  "mcpServers": {
    "mine": {"command": "x"},
    "dec_vault": {"url": "new"}
  }
}
`
	if string(got) != want {
		t.Fatalf("MCP 配置改写不对：\n%s", got)
	}
	if r := resultFor(t, res, mcp); strings.Join(r.Removed, ",") != "dec,dec-old" {
		t.Fatalf("删除的键不对：%v", r.Removed)
	}

	if _, err := os.Stat(key); err != nil {
		t.Fatal("只列出的凭据不应被删")
	}
	if len(res.Listed) != 1 {
		t.Fatalf("应返回只列出项：%+v", res.Listed)
	}
}

// 没有可清理内容时不产生归档（需求 3.3）。
func TestExecuteEmpty(t *testing.T) {
	state, root := archiveFixture(t)
	key := put(t, filepath.Join(root, ".ssh/dec_key"), "KEY", 0o600)
	res, err := Execute(state, &Report{Items: []Item{{Rule: RuleSSH, Action: ActionList, Path: key}}}, execOpts(&fakeProcs{}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Empty || res.Archive != nil {
		t.Fatalf("应为空且无归档：%+v", res)
	}
	if _, err := os.Stat(ArchiveDir(state)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("空结果不应创建归档目录")
	}
}

// 归档失败时一项都不删（需求 4.2）。
func TestExecuteArchiveFailureDeletesNothing(t *testing.T) {
	state, root := archiveFixture(t)
	a := put(t, filepath.Join(root, ".decs/a"), "a", 0o644)
	report := &Report{Items: []Item{
		{Rule: RuleDecsHome, Action: ActionDelete, Path: a},
		{Rule: RuleShellRC, Action: ActionStrip, Path: filepath.Join(root, ".zshrc-missing")},
	}}
	if _, err := Execute(state, report, execOpts(&fakeProcs{})); err == nil {
		t.Fatal("归档失败应返回错误")
	}
	if _, err := os.Stat(a); err != nil {
		t.Fatal("归档失败后不应删除任何内容")
	}
}

// 单项失败不回滚、不中止，其余照常（需求 4.5）。
func TestExecutePartialFailure(t *testing.T) {
	state, root := archiveFixture(t)
	app := filepath.Join(root, "Applications/dec-console.app")
	put(t, filepath.Join(app, "Contents/MacOS/dec-console"), "bin", 0o755)
	other := put(t, filepath.Join(root, ".decs/a"), "a", 0o644)
	report := &Report{Items: []Item{
		{Rule: RuleConsoleApp, Action: ActionDelete, Path: app, IsDir: true},
		{Rule: RuleDecsHome, Action: ActionDelete, Path: other},
	}}
	res, err := Execute(state, report, execOpts(&fakeProcs{err: errors.New("operation not permitted")}))
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(t, res, app); r.Status != StatusFailed || !strings.Contains(r.Reason, "结束进程失败") {
		t.Fatalf("结束进程失败时不应删除 App：%+v", r)
	}
	if _, err := os.Stat(app); err != nil {
		t.Fatal("App 应保留")
	}
	if r := resultFor(t, res, other); r.Status != StatusDeleted {
		t.Fatalf("其他项应照常删除：%+v", r)
	}
	if res.Archive == nil {
		t.Fatal("有失败项时归档仍然有效")
	}
}

// 归档后对象被改过：跳过，不按旧归档删掉新内容。
func TestExecuteSkipsChangedAfterArchive(t *testing.T) {
	state, root := archiveFixture(t)
	dir := filepath.Join(root, ".decs")
	f := put(t, filepath.Join(dir, "a"), "a", 0o644)
	rc := put(t, filepath.Join(root, ".bashrc"), "export PATH=~/.dec/bin:$PATH\n", 0o644)
	report := &Report{Items: []Item{
		{Rule: RuleDecsHome, Action: ActionDelete, Path: dir, IsDir: true},
		{Rule: RuleShellRC, Action: ActionStrip, Path: rc},
	}}
	a, err := WriteArchive(state, report.Items, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	// 归档按路径排序，按路径取回各项。
	itemOf := func(path string) (Item, []ArchiveEntry) {
		for i, it := range a.Manifest.Items {
			if it.Path == path {
				var es []ArchiveEntry
				for _, e := range a.Manifest.Entries {
					if e.Item == i {
						es = append(es, e)
					}
				}
				return it, es
			}
		}
		t.Fatalf("归档里没有 %s", path)
		return Item{}, nil
	}
	dirItem, dirEntries := itemOf(dir)
	rcItem, rcEntries := itemOf(rc)

	// 目录里新增文件。
	put(t, filepath.Join(dir, "new"), "new", 0o644)
	if r := deleteItem(dirItem, dirEntries, &fakeProcs{}); r.Status != StatusSkipped || !strings.Contains(r.Reason, "新增") {
		t.Fatalf("新增文件应跳过：%+v", r)
	}
	_ = os.Remove(filepath.Join(dir, "new"))

	// 文件内容被改。
	later := fixedNow.Add(time.Hour)
	_ = os.WriteFile(f, []byte("changed"), 0o644)
	_ = os.Chtimes(f, later, later)
	if r := deleteItem(dirItem, dirEntries, &fakeProcs{}); r.Status != StatusSkipped {
		t.Fatalf("改过的文件应跳过：%+v", r)
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatal("跳过时不应删除")
	}

	// 共享文件被改。
	_ = os.WriteFile(rc, []byte("export PATH=~/.dec/bin:$PATH\nexport B=2\n"), 0o644)
	if r := stripItem(rcItem, rcEntries); r.Status != StatusSkipped {
		t.Fatalf("改过的共享文件应跳过：%+v", r)
	}
	if got, _ := os.ReadFile(rc); !strings.Contains(string(got), "export B=2") {
		t.Fatal("跳过时共享文件不应被改")
	}
}

// 已经不存在的对象算作已删除，不报失败。
func TestExecuteAlreadyGone(t *testing.T) {
	state, root := archiveFixture(t)
	f := put(t, filepath.Join(root, ".decs/a"), "a", 0o644)
	a, err := WriteArchive(state, []Item{{Rule: RuleDecsHome, Action: ActionDelete, Path: f}}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(f)
	if r := deleteItem(a.Manifest.Items[0], a.Manifest.Entries, &fakeProcs{}); r.Status != StatusDeleted {
		t.Fatalf("已不存在应算删除：%+v", r)
	}
}

// 删除软链接只删链接，不碰目标。
func TestExecuteSymlinkNotFollowed(t *testing.T) {
	state, root := archiveFixture(t)
	target := put(t, filepath.Join(root, "dotfiles/skill/SKILL.md"), "keep", 0o644)
	link := filepath.Join(root, ".claude/skills/dec-x")
	_ = os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(filepath.Dir(target), link); err != nil {
		t.Fatal(err)
	}
	res, err := Execute(state, &Report{Items: []Item{{Rule: RuleUserHostItems, Action: ActionDelete, Path: link}}}, execOpts(&fakeProcs{}))
	if err != nil || res.Failed() {
		t.Fatalf("%v %+v", err, res)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("链接应已删除")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("链接目标不应被删")
	}
}

// H15 只删托管块之外的旧版 Include，块内与用户的行原样保留。
func TestStripSSHIncludes(t *testing.T) {
	in := "Include config.d/dec.conf\r\n# BEGIN DEC MANAGED\nInclude config.d/dec.conf\n# END DEC MANAGED\nHost a\n  Include ~/.ssh/config.d/dec-project-x.conf\n  Include other.conf\n"
	out, removed := stripSSHIncludes([]byte(in))
	want := "# BEGIN DEC MANAGED\nInclude config.d/dec.conf\n# END DEC MANAGED\nHost a\n  Include other.conf\n"
	if string(out) != want || len(removed) != 2 {
		t.Fatalf("结果不对：%q %v", out, removed)
	}
	if out, removed := stripSSHIncludes([]byte("Include config.d/dec.conf\n")); len(removed) != 0 || string(out) != "Include config.d/dec.conf\n" {
		t.Fatal("没有托管块时不应删任何行")
	}
}

func TestStripJSONServers(t *testing.T) {
	cases := []struct {
		name, in, want string
		drop           []string
	}{
		{"删第一个", `{"mcpServers":{"dec":1,"a":2}}`, `{"mcpServers":{"a":2}}`, []string{"dec"}},
		{"删中间", `{"mcpServers":{"a":1,"dec-x":2,"b":3}}`, `{"mcpServers":{"a":1,"b":3}}`, []string{"dec-x"}},
		{"删最后", `{"mcpServers":{"a":1, "dec":2}}`, `{"mcpServers":{"a":1}}`, []string{"dec"}},
		{"全删", "{\n  \"mcpServers\": {\n    \"dec\": {}\n  },\n  \"x\": 1\n}", "{\n  \"mcpServers\": {},\n  \"x\": 1\n}", []string{"dec"}},
		{"两个容器", `{"mcp_servers":{"dec":1},"mcpServers":{"dec":2,"k":3}}`, `{"mcp_servers":{},"mcpServers":{"k":3}}`, []string{"dec"}},
		{"顶层同名键不动", `{"dec":1,"mcpServers":{"a":1}}`, `{"dec":1,"mcpServers":{"a":1}}`, []string{"dec"}},
		{"转义键名", `{"mcpServers":{"dec\u002dold":1,"a":2}}`, `{"mcpServers":{"a":2}}`, []string{"dec-old"}},
	}
	for _, c := range cases {
		got, _, err := stripJSONServers([]byte(c.in), c.drop)
		if err != nil {
			t.Fatalf("%s：%v", c.name, err)
		}
		if string(got) != c.want {
			t.Fatalf("%s：\n得到 %s\n应为 %s", c.name, got, c.want)
		}
		var probe any
		if json.Unmarshal(got, &probe) != nil {
			t.Fatalf("%s：结果不是合法 JSON", c.name)
		}
	}
	if _, _, err := stripJSONServers([]byte(`{"mcpServers":`), []string{"dec"}); err == nil {
		t.Fatal("坏 JSON 应拒绝改写")
	}
}

func TestStripTOMLServers(t *testing.T) {
	in := `model = "x"

[mcp_servers.dec]
url = "old"

[mcp_servers.dec.env]
K = "v"

[mcp_servers."dec-old"]
command = "y"

[mcp_servers.dec_vault]
url = "new"
# 用户注释
`
	want := `model = "x"

[mcp_servers.dec_vault]
url = "new"
# 用户注释
`
	got, removed := stripTOMLServers([]byte(in), []string{"dec", "dec-old"})
	if string(got) != want {
		t.Fatalf("得到：\n%s", got)
	}
	if strings.Join(removed, ",") != "dec,dec-old" {
		t.Fatalf("删除的表不对：%v", removed)
	}
}

func TestRunningWithin(t *testing.T) {
	root := "/Applications/dec-console.app"
	for args, want := range map[string]bool{
		"/Applications/dec-console.app/Contents/MacOS/dec-console":        true,
		"/Applications/dec-console.app/Contents/MacOS/dec-console --flag": true,
		"/Applications/dec-console.app.bak/x":                             false,
		"tail -f /Applications/dec-console.app/log":                       false,
	} {
		if runningWithin(args, root) != want {
			t.Fatalf("%q 应为 %v", args, want)
		}
	}
}

// 真进程：在临时目录里现编一个常驻程序跑起来，StopWithin 能结束它，且不碰目录外的进程。
// 不拷贝系统的 sleep：macOS 会直接杀掉被拷走的系统签名程序。
func TestStopWithinRealProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不结束进程")
	}
	if testing.Short() {
		t.Skip("需要编译")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("没有 go")
	}
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("没有 sleep")
	}
	dir := t.TempDir()
	src := put(t, filepath.Join(dir, "src", "main.go"), "package main\n\nimport \"time\"\n\nfunc main() { time.Sleep(30 * time.Second) }\n", 0o644)
	bin := filepath.Join(dir, "bin", "dec-old")
	build := sysproc.Command(goBin, "build", "-o", bin, src)
	build.Env = append(os.Environ(), "GO111MODULE=off", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("编译测试程序失败：%v %s", err, out)
	}

	inside := sysproc.Command(bin, "30")
	outside := sysproc.Command(sleepBin, "30")
	if err := inside.Start(); err != nil {
		t.Skip("拷贝的 sleep 无法运行：", err)
	}
	if err := outside.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outside.Process.Kill(); _ = outside.Wait() }()
	done := make(chan error, 1)
	go func() { done <- inside.Wait() }()

	p := &psProcesses{list: listPS, grace: 2 * time.Second, self: os.Getpid()}
	stopped, err := p.StopWithin(filepath.Join(dir, "bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 {
		t.Fatalf("应结束 1 个进程，实际 %v", stopped)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("目录内进程没有退出")
	}
	if outside.ProcessState != nil {
		t.Fatal("目录外进程不应受影响")
	}
}


// TestExecuteReportsProgress 锁住进度回调的阶段与递增（2026-10-29 用户反馈：
// 清理跑很久没有任何进度提示，于是重复点击，而排队的那次随后照样执行）。
func TestExecuteReportsProgress(t *testing.T) {
	state, root := archiveFixture(t)
	decDir := filepath.Join(root, ".dec", "cache")
	put(t, filepath.Join(decDir, "x"), "x", 0o644)
	rcReal := put(t, filepath.Join(root, "dotfiles/zshrc"), "export A=1\r\n# Dec\r\nexport B=2\r\n", 0o640)
	rc := filepath.Join(root, ".zshrc")
	if err := os.Symlink(rcReal, rc); err != nil {
		t.Fatal(err)
	}
	key := put(t, filepath.Join(root, ".ssh/dec_key"), "KEY", 0o600)

	report := &Report{Items: []Item{
		{Rule: RuleDecHome, Action: ActionDelete, Path: decDir, IsDir: true},
		{Rule: RuleShellRC, Action: ActionStrip, Path: rc, Entries: []string{"stale line numbers"}},
		{Rule: RuleSSH, Action: ActionList, Path: key},
	}}

	var got []Progress
	res, err := Execute(state, report, ExecOptions{
		Now:   func() time.Time { return fixedNow },
		Procs: &fakeProcs{},
		Progress: func(p Progress) {
			got = append(got, p)
		},
	})
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("没有上报任何进度：长时间运行的清理在界面上会没有任何反馈")
	}

	// 三个阶段都要出现，且顺序是归档 → 清理 → 提交。
	idx := func(step string) int {
		for i, p := range got {
			if p.Step == step {
				return i
			}
		}
		return -1
	}
	for _, s := range []string{StepArchive, StepClean, StepCommit} {
		if idx(s) < 0 {
			t.Errorf("缺少 %s 阶段的进度，实际收到：%+v", s, got)
		}
	}
	if !(idx(StepArchive) < idx(StepClean) && idx(StepClean) < idx(StepCommit)) {
		t.Errorf("阶段顺序应为 归档→清理→提交，实际：%+v", got)
	}
	// 第一帧就是归档：用户一开始就知道进了哪一步，而不是面对无反馈的等待。
	if got[0].Step != StepArchive {
		t.Errorf("第一帧应是归档阶段，实际是 %+v", got[0])
	}

	// 清理阶段的计数必须单调递增并走到总数，否则进度条会倒退或走不满。
	var clean []Progress
	for _, p := range got {
		if p.Step == StepClean {
			clean = append(clean, p)
		}
	}
	if len(clean) == 0 {
		t.Fatal("没有清理阶段的进度")
	}
	last := clean[len(clean)-1]
	if last.Total == 0 {
		t.Errorf("清理阶段总数为 0，界面算不出百分比：%+v", clean)
	}
	if last.Done != last.Total {
		t.Errorf("清理阶段结束时 done=%d total=%d，进度条走不满", last.Done, last.Total)
	}
	for i := 1; i < len(clean); i++ {
		if clean[i].Done < clean[i-1].Done {
			t.Errorf("清理进度回退：%+v", clean)
			break
		}
	}

	// 归档分母不应把「仅列出」的项算进去：它们不进归档，
	// 算进去会让进度条走一半就跳完。
	var archiveTotal int
	for _, p := range got {
		if p.Step == StepArchive {
			archiveTotal = p.Total
		}
	}
	if archiveTotal != 2 {
		t.Errorf("归档分母应为 2（不含仅列出的 SSH 项），实际 %d", archiveTotal)
	}
	_ = res
}

// TestExecuteProgressNilSafe 保证不传进度回调时行为完全不变。
func TestExecuteProgressNilSafe(t *testing.T) {
	state, root := archiveFixture(t)
	dir := filepath.Join(root, ".dec", "cache")
	put(t, filepath.Join(dir, "x"), "x", 0o644)
	report := &Report{Items: []Item{{Rule: RuleDecHome, Action: ActionDelete, Path: dir, IsDir: true}}}
	res, err := Execute(state, report, execOpts(&fakeProcs{}))
	if err != nil {
		t.Fatalf("未传进度回调时执行失败: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].Status != StatusDeleted {
		t.Fatalf("未传进度回调时结果应不变：%+v", res.Items)
	}
}
