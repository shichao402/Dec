package legacy

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/notice"
	"github.com/shichao402/Dec/internal/proto"
)

// 闭环测试（任务 7）：在临时 HOME 上搭一台“装过旧版又装了新代理”的机器，
// 用真实扫描、真实归档、真实 git 走完 扫描→归档→删除→再扫→还原→保留期，
// 每一步之后都对整棵目录做快照比对，确认没有多删、没有漏还原。

type snapFile struct {
	content string
	mode    fs.FileMode
	link    string
}

// snapshot 记下 root 下所有文件与软链接；跳过 .git、裸远端和代理状态目录（归档自己会变）。
func snapshot(t *testing.T, root string, skip ...string) map[string]snapFile {
	t.Helper()
	out := map[string]snapFile{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, s := range skip {
			if p == s {
				return filepath.SkipDir
			}
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		f := snapFile{mode: info.Mode()}
		if info.Mode()&fs.ModeSymlink != 0 {
			f.link, _ = os.Readlink(p)
		} else {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			f.content = string(b)
		}
		out[p] = f
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type logCollector struct {
	mu    sync.Mutex
	lines []string
}

func (l *logCollector) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logCollector) has(t *testing.T, parts ...string) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		ok := true
		for _, p := range parts {
			if !strings.Contains(line, p) {
				ok = false
				break
			}
		}
		if ok {
			return
		}
	}
	t.Fatalf("日志里应有一行同时包含 %q，实际：\n%s", parts, strings.Join(l.lines, "\n"))
}

type loopMachine struct {
	*fixture
	state, remote string
	keepSkill     string
	sshKey        string
	userMCP, rc   string
	projMCP       string
	logs          *logCollector
	svc           *Service
}

func newLoopMachine(t *testing.T) *loopMachine {
	t.Helper()
	requireGit(t)
	f := newFixture(t)
	t.Setenv("HOME", f.home)
	m := &loopMachine{fixture: f, logs: &logCollector{}}
	m.state = f.h(".dec-agent")

	// 新代理自己的东西与新系统落点：一样都不能动。
	f.write(f.h(".dec-agent/agent.json"), `{"device":"dev-x"}`)
	m.keepSkill = f.write(f.h(".claude/skills/forge-robot/SKILL.md"), "---\n---\n"+notice.Text+"\n")
	f.write(f.h(".claude/skills/my-own/SKILL.md"), "mine")
	f.write(f.h(".dec/logs/auth.log"), "login: start")

	// 旧版用户级残留。
	f.write(f.h(".dec/config.yaml"), "managed_projects:\n  - root: "+f.project+"\n")
	f.write(f.h(".dec/cache/bundle/a.md"), "cached")
	f.write(f.h(".decs/bin/dec"), "old binary")
	f.write(f.h(".claude/skills/dec-old/SKILL.md"), "old skill")
	m.rc = f.write(f.h(".zshrc"), "export A=1\n# Dec\nexport PATH=\"/u/.dec/bin:$PATH\"\nalias ll='ls -l'\n")
	m.userMCP = f.write(f.h(".cursor/mcp.json"), "{\n  \"mcpServers\": {\n    \"dec-old\": {\"command\": \"x\"},\n    \"dec-tencent-cloud\": {\"url\": \"new\"},\n    \"github\": {\"command\": \"gh\"}\n  }\n}\n")
	// 旧版落下但还在用的凭据：只列出。
	m.sshKey = f.write(f.h(".ssh/dec_cloud_ops"), "KEY")

	// 旧版管理的项目：只登记在旧版 managed_projects 里，被 git 跟踪，有上游。
	m.remote = filepath.Join(filepath.Dir(f.project), "remote.git")
	git(t, filepath.Dir(f.project), "init", "--quiet", "--bare", "-b", "main", m.remote)
	git(t, f.project, "init", "--quiet", "-b", "main")
	setIdentity(t, f.project)
	f.write(f.p(".dec/config.yaml"), "old: 1\n")
	f.write(f.p("app.go"), "package app\n")
	m.projMCP = f.write(f.p(".mcp.json"), "{\n  \"mcpServers\": {\n    \"dec-old\": {\"command\": \"x\"},\n    \"keep\": {\"command\": \"y\"}\n  }\n}\n")
	git(t, f.project, "add", "-A")
	git(t, f.project, "commit", "--quiet", "-m", "init")
	git(t, f.project, "remote", "add", "origin", m.remote)
	git(t, f.project, "push", "--quiet", "-u", "origin", "main")

	expected := []proto.ManifestEntry{
		{ID: "skill", Paths: []string{m.keepSkill}, Format: proto.FormatWhole},
		{ID: "mcp", Paths: []string{m.userMCP}, Format: proto.FormatJSONKey, Key: "dec-tencent-cloud"},
	}
	m.svc = &Service{
		StateDir: m.state,
		ScanFn: func() (*Report, error) {
			return Scan(Input{
				Home:         f.home,
				Expected:     NewExpected(expected),
				GOOS:         "darwin",
				Applications: []string{f.apps},
				SystemBin:    f.sysbin,
			})
		},
		Logf:  m.logs.logf,
		Procs: &fakeProcs{},
		Now:   func() time.Time { return fixedNow },
		Git:   GitOptions{NetTimeout: 20 * time.Second},
	}
	return m
}

func (m *loopMachine) snap(t *testing.T) map[string]snapFile {
	return snapshot(t, filepath.Dir(m.home), m.state, m.remote)
}

func TestLoopScanArchiveDeleteRestore(t *testing.T) {
	m := newLoopMachine(t)
	before := m.snap(t)

	// 扫描：项目来自旧版 managed_projects；新系统落点、凭据都不进清理范围。
	v, err := m.svc.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if v.Empty || len(v.Projects) != 1 || v.Projects[0] != m.project {
		t.Fatalf("扫描结果不对：projects=%v empty=%v", v.Projects, v.Empty)
	}
	if it, ok := find(v.Report, m.sshKey); !ok || it.Action != ActionList {
		t.Fatalf("SSH 密钥应只列出：%+v", it)
	}
	m.logs.has(t, "旧版残留扫描")

	res, err := m.svc.Execute(v.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed() || res.Archive == nil {
		t.Fatalf("执行不应有失败项：%+v", res.Items)
	}
	id := res.Archive.ID

	// 删除后：逐个核对哪些没了、哪些只少了旧条目、哪些原样。
	after := m.snap(t)
	gone := []string{
		m.h(".dec/config.yaml"), m.h(".dec/cache/bundle/a.md"), m.h(".decs/bin/dec"),
		m.h(".claude/skills/dec-old/SKILL.md"), m.p(".dec/config.yaml"),
	}
	for _, p := range gone {
		if _, ok := after[p]; ok {
			t.Errorf("%s 应已删除", p)
		}
	}
	stripped := map[string][2]string{
		m.rc:      {"/.dec/bin", "alias ll"},
		m.userMCP: {"dec-old", "dec-tencent-cloud"},
		m.projMCP: {"dec-old", "keep"},
	}
	for p, w := range stripped {
		got := after[p].content
		if strings.Contains(got, w[0]) || !strings.Contains(got, w[1]) {
			t.Errorf("%s 应只删掉 %q、保留 %q，实际：\n%s", p, w[0], w[1], got)
		}
	}
	for p, f := range before {
		if _, isStrip := stripped[p]; isStrip {
			continue
		}
		isGone := false
		for _, g := range gone {
			isGone = isGone || g == p
		}
		if !isGone && after[p] != f {
			t.Errorf("%s 不在清理范围，却被改动了", p)
		}
	}
	for _, p := range []string{m.h(".dec"), m.p(".dec")} {
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			t.Errorf("%s 本身应保留，只清里面的内容", p)
		}
	}
	if len(after) != len(before)-len(gone) {
		t.Errorf("清理后不应多出文件：前 %d 后 %d 删 %d", len(before), len(after), len(gone))
	}

	// 项目：一个提交、已推送到上游。
	repo := onlyRepo(t, res)
	if repo.Status != RepoPushed {
		t.Fatalf("项目应已提交并推送：%+v", repo)
	}
	if got := git(t, m.remote, "log", "-1", "--format=%s"); got != CommitSubject {
		t.Fatalf("远端最新提交应为清理提交，实际 %q", got)
	}
	if st := git(t, m.project, "status", "--porcelain"); st != "" {
		t.Fatalf("提交后工作区应干净：%s", st)
	}

	// 日志：归档 id、条目数、仓库结果。
	m.logs.has(t, "旧版残留清理 "+id, strconv.Itoa(len(res.Items))+" 项", "失败 0")
	m.logs.has(t, "旧版残留清理 "+id, "仓库 "+repo.Repo, string(RepoPushed))

	// 再扫：只剩只列出的凭据。
	v2, err := m.svc.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if !v2.Empty {
		t.Fatalf("清理后再扫应干净，实际：%v", paths(v2.Report))
	}
	if _, err := m.svc.Execute(v2.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if list, _ := m.svc.Archives(); len(list) != 1 {
		t.Fatalf("没有残留时不应产生新归档：%+v", list)
	}

	// 整份还原：删掉的回到原处、逐字节一致；改写过的写到旁边，现有文件不被覆盖。
	rr, err := m.svc.Restore(id, nil)
	if err != nil || rr.Failed() {
		t.Fatalf("还原应全部成功：%v %+v", err, rr)
	}
	restored := m.snap(t)
	suffix := ".restored-" + fixedNow.UTC().Format("20060102T150405Z")
	for p, f := range before {
		if _, isStrip := stripped[p]; isStrip {
			if restored[p] != after[p] {
				t.Errorf("%s 当前内容不应被还原覆盖", p)
			}
			if restored[p+suffix].content != f.content {
				t.Errorf("%s 的原文应还原到 %s", p, p+suffix)
			}
			continue
		}
		got, ok := restored[p]
		if !ok {
			t.Errorf("%s 没有还原", p)
			continue
		}
		if got.content != f.content || got.link != f.link || (runtime.GOOS != "windows" && got.mode != f.mode) {
			t.Errorf("%s 还原后与原来不一致：%+v vs %+v", p, got, f)
		}
	}
	if len(restored) != len(before)+len(stripped) {
		t.Errorf("还原后文件数应为原来的 %d 加 %d 个旁写文件，实际 %d", len(before), len(stripped), len(restored))
	}
	// 还原不替用户提交：项目里的旧文件回到工作区，未跟踪。
	if st := git(t, m.project, "status", "--porcelain", "--", ".dec"); !strings.Contains(st, "?? .dec/") {
		t.Fatalf("还原的项目文件应留在工作区由用户决定，实际 %q", st)
	}
	if got := git(t, m.remote, "log", "-1", "--format=%s"); got != CommitSubject {
		t.Fatal("还原不应产生提交或推送")
	}
	m.logs.has(t, "还原归档 "+id, "失败 0")

	// 保留期：365 天内不清，过了才清，并写日志。
	if gone, err := PurgeExpired(m.state, fixedNow.AddDate(0, 0, RetentionDays-1)); err != nil || len(gone) != 0 {
		t.Fatalf("保留期内不应清除：%v %v", gone, err)
	}
	if gone, err := PurgeExpired(m.state, fixedNow.AddDate(0, 0, RetentionDays+1)); err != nil || len(gone) != 1 || gone[0] != id {
		t.Fatalf("过期应清除 %s：%v %v", id, gone, err)
	}
	if list, _ := m.svc.Archives(); len(list) != 0 {
		t.Fatalf("过期后归档应已清除：%+v", list)
	}
}

// 删除失败的条目写进日志，带归档 id、路径和原因；其余条目照常处理（需求 7.1）。
func TestLoopLogsFailures(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("需要用目录权限制造删除失败")
	}
	m := newLoopMachine(t)
	locked := m.h(".decs/bin")
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	v, err := m.svc.Scan()
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.svc.Execute(v.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed() {
		t.Fatal("应有删除失败的条目")
	}
	var failed ItemResult
	for _, it := range res.Items {
		if it.Status == StatusFailed {
			failed = it
		} else if it.Status != StatusDeleted && it.Status != StatusStripped {
			t.Errorf("其余条目应照常处理：%+v", it)
		}
	}
	if failed.Item.Path != m.h(".decs") {
		t.Fatalf("失败项应为 ~/.decs：%+v", failed)
	}
	id := res.Archive.ID
	m.logs.has(t, "旧版残留清理 "+id, string(StatusFailed), m.h(".decs"), failed.Reason)
	m.logs.has(t, "旧版残留清理 "+id, "失败 1")
	// 失败那项仍在归档里，可以从归档取回。
	av, err := m.svc.Archive(id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range av.Items {
		found = found || it.Path == m.h(".decs")
	}
	if !found {
		t.Fatal("失败项也应已归档")
	}
}

// 日志只写本地：legacy 包不直接接触网络，也不引用会连云端的内部包（需求 7.1「不上报云端」）。
// git 推送是唯一的出站动作，走 sysproc 起的 git 子进程，受需求 8 约束。
func TestLegacyPackageHasNoNetwork(t *testing.T) {
	allowedInternal := map[string]bool{
		"github.com/shichao402/Dec/internal/notice":  true,
		"github.com/shichao402/Dec/internal/proto":   true,
		"github.com/shichao402/Dec/internal/sysproc": true,
	}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == "net" || strings.HasPrefix(p, "net/") {
				t.Errorf("%s 不应引用 %s", name, p)
			}
			if strings.HasPrefix(p, "github.com/shichao402/Dec/") && !allowedInternal[p] {
				t.Errorf("%s 引用了 %s，确认它不会把清理信息带出本机后再加进白名单", name, p)
			}
		}
	}
}
