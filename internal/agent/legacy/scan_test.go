package legacy

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/notice"
	"github.com/shichao402/Dec/internal/proto"
)

// fixture 在临时目录里搭一台“装过旧版又装了新代理”的机器。
type fixture struct {
	t       *testing.T
	home    string
	project string
	apps    string
	sysbin  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{
		t:       t,
		home:    filepath.Join(root, "home"),
		project: filepath.Join(root, "work", "proj"),
		apps:    filepath.Join(root, "Applications"),
		sysbin:  filepath.Join(root, "usr-local-bin"),
	}
	for _, d := range []string{f.home, f.project, f.apps, f.sysbin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *fixture) write(path, content string) string {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func (f *fixture) h(rel string) string { return filepath.Join(f.home, filepath.FromSlash(rel)) }
func (f *fixture) p(rel string) string { return filepath.Join(f.project, filepath.FromSlash(rel)) }

func (f *fixture) scan(expected []proto.ManifestEntry, projects ...string) *Report {
	f.t.Helper()
	r, err := Scan(Input{
		Home:         f.home,
		Projects:     projects,
		Expected:     NewExpected(expected),
		GOOS:         "darwin",
		Applications: []string{f.apps},
		SystemBin:    f.sysbin,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func find(r *Report, path string) (Item, bool) {
	for _, it := range r.Items {
		if it.Path == path {
			return it, true
		}
	}
	return Item{}, false
}

func mustHit(t *testing.T, r *Report, path, rule string, action Action) Item {
	t.Helper()
	it, ok := find(r, path)
	if !ok {
		t.Fatalf("应命中 %s（%s），实际没有。命中：%v", path, rule, paths(r))
	}
	if it.Rule != rule || it.Action != action {
		t.Fatalf("%s 应为 %s/%s，实际 %s/%s", path, rule, action, it.Rule, it.Action)
	}
	return it
}

func mustMiss(t *testing.T, r *Report, path string) {
	t.Helper()
	for _, it := range r.Items {
		if it.Action == ActionList {
			continue
		}
		if overlaps(it.Path, path) {
			t.Fatalf("%s 不应进入清理范围，却被 %s 命中为 %s", path, it.Rule, it.Path)
		}
	}
}

func paths(r *Report) []string {
	var out []string
	for _, it := range r.Items {
		out = append(out, it.Rule+" "+it.Path)
	}
	sort.Strings(out)
	return out
}

// 拿不到云端期望条目时必须拒绝（需求 2.3.1）。
func TestScanRefusesWithoutExpected(t *testing.T) {
	f := newFixture(t)
	_, err := Scan(Input{Home: f.home})
	if !errors.Is(err, ErrExpectedUnknown) {
		t.Fatalf("应返回 ErrExpectedUnknown，实际 %v", err)
	}
}

// 新代理自身永远排除（需求 2.3 第一项）。
func TestNewAgentAlwaysExcluded(t *testing.T) {
	f := newFixture(t)
	agentBin := f.write(f.h(".local/bin/dec-agent"), "bin")
	trayBin := f.write(f.h(".local/bin/dec-tray"), "bin")
	elsewhere := f.write(filepath.Join(f.sysbin, "dec-agent"), "bin")
	f.write(f.h("Library/LaunchAgents/com.dec.agent.plist"),
		"<plist><string>"+elsewhere+"</string><string>"+f.h(".dec-agent")+"</string></plist>")
	f.write(f.h("Library/LaunchAgents/com.dec.tray.plist"), "<plist/>")
	f.write(f.h(".dec-agent/config.json"), "{}")
	// 旧版二进制和新代理放在同一目录，必须只删旧的。
	oldBin := f.write(f.h(".local/bin/dec-server"), "bin")

	r := f.scan(nil)
	for _, p := range []string{agentBin, trayBin, elsewhere, f.h(".dec-agent"),
		f.h("Library/LaunchAgents/com.dec.agent.plist"), f.h("Library/LaunchAgents/com.dec.tray.plist")} {
		mustMiss(t, r, p)
	}
	mustHit(t, r, oldBin, RuleOldBinaries, ActionDelete)
	var sawSkip bool
	for _, s := range r.Skipped {
		if s.Path == elsewhere {
			sawSkip = true
		}
	}
	if !sawSkip {
		t.Fatalf("启动项引用的 %s 应记为被排除，实际 Skipped=%v", elsewhere, r.Skipped)
	}
}

// ~/.dec 与项目 .dec 只清内容，不删目录本身（需求 2.3 第四项）。
func TestRootDecKeptButContentsCleared(t *testing.T) {
	f := newFixture(t)
	f.write(f.h(".dec/config.yaml"), "managed_projects: []\n")
	f.write(f.h(".dec/repo.git/HEAD"), "ref")
	authLog := f.write(f.h(".dec/logs/auth.log"), "login: start")
	f.write(f.p(".dec/config.yaml"), "x")

	r := f.scan(nil, f.project)
	mustMiss(t, r, authLog)
	for _, it := range r.Items {
		if it.Path == f.h(".dec") || it.Path == f.p(".dec") {
			t.Fatalf("落点根目录本身不能被整删：%s", it.Path)
		}
	}
	mustHit(t, r, f.h(".dec/config.yaml"), RuleDecHome, ActionDelete)
	mustHit(t, r, f.h(".dec/repo.git"), RuleDecHome, ActionDelete)
	mustHit(t, r, f.p(".dec/config.yaml"), RuleProjectDec, ActionDelete)
}

// 云端期望的落点与带说明行的文件不进清理范围，即使名字是 dec-*（需求 2.3 第二、三项）。
func TestNewSystemLandingsExcluded(t *testing.T) {
	f := newFixture(t)
	// 新系统下发的 env 文件不带说明行，只能靠期望清单认出来。
	env := f.write(f.h(".dec/env/tencent.toml"), "secret")
	// 新系统落的 skill 带说明行，名字恰好也是 dec- 前缀。
	newSkill := f.write(f.h(".claude/skills/dec-new/SKILL.md"), "---\n---\n"+notice.Text+"\n")
	expectedSkill := f.write(f.h(".cursor/skills/dec-cloud/SKILL.md"), "no notice")
	oldSkill := f.write(f.h(".claude/skills/dec-old/SKILL.md"), "old")
	userSkill := f.write(f.h(".claude/skills/my-own/SKILL.md"), "mine")

	r := f.scan([]proto.ManifestEntry{
		{ID: "a", Paths: []string{env}, Format: proto.FormatWhole},
		{ID: "b", Paths: []string{expectedSkill}, Format: proto.FormatWhole},
	})
	mustMiss(t, r, env)
	mustMiss(t, r, f.h(".dec/env"))
	mustMiss(t, r, newSkill)
	mustMiss(t, r, expectedSkill)
	mustMiss(t, r, userSkill)
	mustHit(t, r, filepath.Dir(oldSkill), RuleUserHostItems, ActionDelete)
}

// MCP：旧版内置 dec 与不在期望里的 dec-* 被删；dec_vault、期望键、用户自己的键保留。
func TestMCPKeys(t *testing.T) {
	f := newFixture(t)
	cfg := f.write(f.h(".cursor/mcp.json"), `{"mcpServers":{
		"dec":{}, "dec-old":{}, "dec-tencent-cloud":{}, "dec_vault":{}, "github":{}}}`)
	toml := f.write(f.h(".codex/config.toml"), "[mcp_servers.dec]\ncommand='x'\n[mcp_servers.\"dec-old\".env]\nA=1\n[mcp_servers.mine]\n")
	projCfg := f.write(f.p(".mcp.json"), `{"mcpServers":{"dec":{},"dec-old":{}}}`)

	r := f.scan([]proto.ManifestEntry{
		{ID: "m", Paths: []string{cfg}, Format: proto.FormatJSONKey, Key: "dec-tencent-cloud"},
	}, f.project)

	it := mustHit(t, r, cfg, RuleUserMCP, ActionStrip)
	if got := strings.Join(it.Entries, ","); got != "dec,dec-old" {
		t.Fatalf("用户级 MCP 应只删 dec,dec-old，实际 %s", got)
	}
	it = mustHit(t, r, toml, RuleUserMCP, ActionStrip)
	if got := strings.Join(it.Entries, ","); got != "dec,dec-old" {
		t.Fatalf("Codex TOML 应只删 dec,dec-old，实际 %s", got)
	}
	// 项目级的 dec 不是旧版内置入口，不动。
	it = mustHit(t, r, projCfg, RuleProjectMCP, ActionStrip)
	if got := strings.Join(it.Entries, ","); got != "dec-old" {
		t.Fatalf("项目级 MCP 应只删 dec-old，实际 %s", got)
	}
}

// 失效软链接作为一条规则纳入，有效链接和用户条目不动（需求 2.5）。
func TestBrokenLinks(t *testing.T) {
	f := newFixture(t)
	skills := f.h(".bg-agent/config-with-app/skills")
	target := f.write(f.h("real/SKILL.md"), "x")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(skills, "gone")
	alive := filepath.Join(skills, "alive")
	if err := os.Symlink(f.h("nowhere"), broken); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(target), alive); err != nil {
		t.Fatal(err)
	}
	r := f.scan(nil)
	mustHit(t, r, broken, RuleBrokenLinks, ActionDelete)
	mustMiss(t, r, alive)
	mustMiss(t, r, target)
}

// 项目范围是新登记与旧版登记的并集（需求 2.4）。
func TestProjectsUnion(t *testing.T) {
	f := newFixture(t)
	oldOnly := filepath.Join(filepath.Dir(f.project), "old-only")
	f.write(filepath.Join(oldOnly, ".dec/vars.yaml"), "x")
	f.write(f.h(".dec/config.yaml"), "managed_projects:\n  - root: "+oldOnly+"\n  - root: /does/not/exist\n")
	f.write(f.p(".dec/config.yaml"), "x")

	r := f.scan(nil, f.project)
	if got := strings.Join(r.Projects, ","); got != strings.Join([]string{f.project, oldOnly}, ",") &&
		got != strings.Join([]string{oldOnly, f.project}, ",") {
		t.Fatalf("项目范围应为两边并集且去掉不存在的目录，实际 %v", r.Projects)
	}
	mustHit(t, r, filepath.Join(oldOnly, ".dec/vars.yaml"), RuleProjectDec, ActionDelete)
}

// 仍可能在用的旧版凭据只列出，不进入删除与归档（需求 2.6）。
func TestCredentialsOnlyListed(t *testing.T) {
	f := newFixture(t)
	key := f.write(f.h(".ssh/dec_github_github_commit"), "KEY")
	frag := f.write(f.h(".ssh/config.d/dec.conf"), "Host x")
	f.write(f.h(".ssh/config"), "Include config.d/dec.conf\nHost other\n")
	sec := f.write(f.p(".secrets/app/config.yaml"), "s")
	decSec := f.write(f.h(".dec/secrets/.gcm/cnb.yaml"), "s")

	r := f.scan(nil, f.project)
	for _, p := range []string{key, frag, f.h(".ssh/config"), filepath.Dir(filepath.Dir(sec)), filepath.Dir(filepath.Dir(decSec))} {
		it, ok := find(r, p)
		if !ok {
			t.Fatalf("%s 应列出", p)
		}
		if it.Action != ActionList || it.Group != GroupCredential {
			t.Fatalf("%s 应为只列出的凭据，实际 %s/%s", p, it.Action, it.Group)
		}
	}
	mustMiss(t, r, key)
	mustMiss(t, r, sec)
	mustMiss(t, r, decSec)
	if r.ArchiveBytes != 0 {
		t.Fatalf("只有凭据时归档体积应为 0，实际 %d", r.ArchiveBytes)
	}
	if !r.Empty() {
		t.Fatal("只剩凭据时应报告为没有需要清理的旧版残留")
	}
}

// shell 配置只删旧版 PATH 行。
func TestShellRC(t *testing.T) {
	f := newFixture(t)
	rc := f.write(f.h(".zshrc"), "export A=1\n# Dec\nexport PATH=\"/u/.dec/bin:$PATH\"\n# old: /u/.dec/bin\nexport PATH=\"/u/.decs/bin:$PATH\"\n")
	r := f.scan(nil)
	it := mustHit(t, r, rc, RuleShellRC, ActionStrip)
	if len(it.Entries) != 3 {
		t.Fatalf("应命中 3 行（注释标记与两条 PATH），实际 %v", it.Entries)
	}
}

// 旧 Console 与数据只在 macOS 规则里出现。
func TestConsoleDarwinOnly(t *testing.T) {
	f := newFixture(t)
	app := f.write(filepath.Join(f.apps, "dec-console.app/Contents/Info.plist"), "x")
	data := f.write(f.h("Library/Caches/com.shichao402.dec.console/c"), "x")
	r := f.scan(nil)
	mustHit(t, r, filepath.Dir(filepath.Dir(app)), RuleConsoleApp, ActionDelete)
	mustHit(t, r, filepath.Dir(data), RuleConsoleData, ActionDelete)

	r2, err := Scan(Input{Home: f.home, Expected: NewExpected(nil), GOOS: "linux", Applications: []string{f.apps}, SystemBin: f.sysbin})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := find(r2, filepath.Dir(filepath.Dir(app))); ok {
		t.Fatal("非 macOS 不应扫描 Console")
	}
}

// 每条规则都有编号、说明、动作和分组（需求 2.1），编号不重复。
func TestRuleTableWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Rules {
		if r.ID == "" || r.Title == "" || r.Action == "" || r.Group == "" {
			t.Fatalf("规则字段不完整：%+v", r)
		}
		if seen[r.ID] {
			t.Fatalf("规则编号重复：%s", r.ID)
		}
		seen[r.ID] = true
		if r.Group == GroupCredential && r.Action != ActionList {
			t.Fatalf("凭据类规则只能列出：%s", r.ID)
		}
	}
}
