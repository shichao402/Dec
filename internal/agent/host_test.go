package agent

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/proto"
)

// 白名单是代理的第一条硬边界（需求 2.2、2.3）。
//
// 这些用例锁定的不是“能写”，而是“不能写”：云端能决定的只有根名、
// 平面与相对路径，任何试图越出内置表的落点都必须被拒绝。

// testHome 造一个临时用户目录。
func testHome(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestResolveRejectsUnknownHost(t *testing.T) {
	home := testHome(t)
	r := NewResolver(home, nil)
	_, err := r.Resolve(proto.Target{Plan: proto.PlanUser, Root: "vscode", Path: "rules/a.md"})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("未知宿主应被拒绝，实际 err=%v", err)
	}
}

func TestResolveKnownHostUserPlane(t *testing.T) {
	home := testHome(t)
	r := NewResolver(home, nil)
	got, err := r.Resolve(proto.Target{Plan: proto.PlanUser, Root: "cursor", Path: "rules/style.md"})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	want := filepath.Join(home, ".cursor", "rules", "style.md")
	if got != want {
		t.Fatalf("落点 %q，期望 %q", got, want)
	}
}

func TestResolveRejectsPathTraversal(t *testing.T) {
	home := testHome(t)
	r := NewResolver(home, nil)
	for _, p := range []string{
		"rules/../../etc/passwd",
		"../../etc/passwd",
		"skills/../rules/a.md",
	} {
		if _, err := r.Resolve(proto.Target{Plan: proto.PlanUser, Root: "cursor", Path: p}); err == nil {
			t.Fatalf("落点 %q 应被拒绝", p)
		}
	}
}

func TestResolveRejectsUnknownCategory(t *testing.T) {
	home := testHome(t)
	r := NewResolver(home, nil)
	// 只允许 skills / rules / env 三个类别：否则云端可以往宿主目录里
	// 塞任意文件名，白名单就退化成“整个 IDE 目录都可写”。
	if _, err := r.Resolve(proto.Target{Plan: proto.PlanUser, Root: "cursor", Path: "settings.json"}); err == nil {
		t.Fatal("未知类别应被拒绝")
	}
	if _, err := r.Resolve(proto.Target{Plan: proto.PlanUser, Root: "cursor", Path: "rules"}); err == nil {
		t.Fatal("缺少文件名的落点应被拒绝")
	}
}

func TestResolveProjectRequiresRegistration(t *testing.T) {
	home := testHome(t)
	proj := filepath.Join(home, "work", "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("建项目目录失败: %v", err)
	}
	target := proto.Target{Plan: proto.PlanProject, ProjectID: "p1", Root: "claude", Path: "rules/a.md"}

	if _, err := NewResolver(home, nil).Resolve(target); err == nil {
		t.Fatal("未登记的项目应被拒绝")
	}
	got, err := NewResolver(home, []Project{{ID: "p1", Path: proj}}).Resolve(target)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	want := filepath.Join(proj, ".claude", "rules", "a.md")
	if got != want {
		t.Fatalf("落点 %q，期望 %q", got, want)
	}
}

func TestResolveRejectsProjectOutsideHome(t *testing.T) {
	home := testHome(t)
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	r := NewResolver(home, []Project{{ID: "p1", Path: outside}})
	_, err := r.Resolve(proto.Target{Plan: proto.PlanProject, ProjectID: "p1", Root: "claude", Path: "rules/a.md"})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("用户目录之外的项目根应被拒绝，实际 err=%v", err)
	}
}

func TestResolveMCPOnlyForJSONHosts(t *testing.T) {
	home := testHome(t)
	r := NewResolver(home, nil)

	got, err := r.Resolve(proto.Target{Plan: proto.PlanUser, Root: "cursor", Path: FileMCP})
	if err != nil {
		t.Fatalf("cursor 的 MCP 落点应可解析: %v", err)
	}
	if got != filepath.Join(home, ".cursor", FileMCP) {
		t.Fatalf("cursor MCP 落点 %q", got)
	}

	// codex 的 MCP 配置是 TOML，协议层没有段合并语义。
	if _, err := r.Resolve(proto.Target{Plan: proto.PlanUser, Root: "codex", Path: FileMCP}); err == nil {
		t.Fatal("codex 的 MCP 落点应被拒绝")
	}
}

func TestResolveAbsoluteOnlyForDelete(t *testing.T) {
	home := testHome(t)
	r := NewResolver(home, nil)
	inside := filepath.Join(home, ".cursor", "rules", "a.md")

	got, err := r.Resolve(proto.Target{Plan: proto.PlanUser, Path: inside})
	if err != nil {
		t.Fatalf("用户目录内的绝对路径应可解析: %v", err)
	}
	if got != inside {
		t.Fatalf("绝对路径落点 %q，期望 %q", got, inside)
	}

	for _, p := range []string{"/etc/passwd", home, string(filepath.Separator)} {
		if _, err := r.Resolve(proto.Target{Plan: proto.PlanUser, Path: p}); err == nil {
			t.Fatalf("绝对路径 %q 应被拒绝", p)
		}
	}
	if _, err := r.Resolve(proto.Target{Plan: proto.PlanUser, Path: "relative/a.md"}); err == nil {
		t.Fatal("相对路径不应走绝对路径分支")
	}
}

func TestHostNamesIncludeDecRoot(t *testing.T) {
	names := strings.Join(HostNames(), ",")
	for _, want := range []string{"cursor", "claude", "codebuddy", "codex", "with", RootDec} {
		if !strings.Contains(names, want) {
			t.Fatalf("白名单缺少宿主 %q：%s", want, names)
		}
	}
}

func TestCleanRelRejectsParent(t *testing.T) {
	for _, p := range []string{"", ".", "..", "../a", "a/../../b"} {
		if _, ok := cleanRel(p); ok {
			t.Fatalf("%q 应被拒绝", p)
		}
	}
	if got, ok := cleanRel("skills/demo/SKILL.md"); !ok || got != "skills/demo/SKILL.md" {
		t.Fatalf("正常相对路径被改写: %q ok=%v", got, ok)
	}
}

func TestOSArchNonEmpty(t *testing.T) {
	osName, arch := OSArch()
	if osName != runtime.GOOS || arch != runtime.GOARCH {
		t.Fatalf("平台标识不符: %s/%s", osName, arch)
	}
}
