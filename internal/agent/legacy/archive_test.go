package legacy

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)

func archiveFixture(t *testing.T) (state, root string) {
	t.Helper()
	base := t.TempDir()
	state = filepath.Join(base, "state")
	root = filepath.Join(base, "home")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return state, root
}

func put(t *testing.T, path, content string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func shaOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// readMembers 解开归档，返回成员名到 (头, 内容)。
func readMembers(t *testing.T, path string) map[string]struct {
	hdr  *tar.Header
	body string
} {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]struct {
		hdr  *tar.Header
		body string
	}{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out[strings.TrimSuffix(h.Name, "/")] = struct {
			hdr  *tar.Header
			body string
		}{h, string(b)}
	}
	return out
}

func entryAt(m *Manifest, path string) (ArchiveEntry, bool) {
	for _, e := range m.Entries {
		if e.Path == path {
			return e, true
		}
	}
	return ArchiveEntry{}, false
}

// 往返：文件、目录、软链接（不跟随）、共享文件、只列出项，逐项核对 manifest 与归档内容（需求 4.1、4.4）。
func TestWriteArchiveRoundTrip(t *testing.T) {
	state, root := archiveFixture(t)
	cfg := put(t, filepath.Join(root, ".dec/config.yaml"), "managed_projects: []\n", 0o644)
	secret := put(t, filepath.Join(root, ".dec/cache/secret.txt"), "TOKEN", 0o600)
	link := filepath.Join(root, ".cursor/skills/gone")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "nowhere"), link); err != nil {
		t.Fatal(err)
	}
	// 指向真实目录的链接也只归档链接本身，目录内容不能被带进来。
	real := put(t, filepath.Join(root, "dotfiles/real-skill/SKILL.md"), "keep me", 0o644)
	alive := filepath.Join(root, ".cursor/skills/dec-alive")
	if err := os.Symlink(filepath.Dir(real), alive); err != nil {
		t.Fatal(err)
	}
	// ~/.zshrc 常常是指向 dotfiles 的软链接：归档链接指向的全文，并记录链接。
	rcReal := put(t, filepath.Join(root, "dotfiles/zshrc"), "export A=1\n# Dec\nexport PATH=~/.dec/bin:$PATH\n", 0o644)
	rc := filepath.Join(root, ".zshrc")
	if err := os.Symlink(rcReal, rc); err != nil {
		t.Fatal(err)
	}
	listed := put(t, filepath.Join(root, ".ssh/dec_key"), "KEY", 0o600)

	items := []Item{
		{Rule: RuleDecHome, Action: ActionDelete, Path: cfg},
		{Rule: RuleDecHome, Action: ActionDelete, Path: filepath.Dir(secret), IsDir: true},
		{Rule: RuleBrokenLinks, Action: ActionDelete, Path: link},
		{Rule: RuleUserHostItems, Action: ActionDelete, Path: alive},
		{Rule: RuleShellRC, Action: ActionStrip, Path: rc, Entries: []string{"2: # Dec"}},
		{Rule: RuleSSH, Action: ActionList, Path: listed},
	}
	a, err := WriteArchive(state, items, fixedNow)
	if err != nil {
		t.Fatal(err)
	}

	if a.ID != "20261028T093000Z" {
		t.Fatalf("归档 id 应为 UTC 时间戳，实际 %s", a.ID)
	}
	if got := a.Manifest.ExpiresAt; !got.Equal(fixedNow.AddDate(0, 0, 365)) {
		t.Fatalf("到期日应为 365 天后，实际 %s", got)
	}
	if len(a.Manifest.Items) != 5 {
		t.Fatalf("只列出的命中不应进入归档，实际 %d 项", len(a.Manifest.Items))
	}
	if _, ok := entryAt(a.Manifest, listed); ok {
		t.Fatal("只列出的凭据被归档了")
	}

	members := readMembers(t, a.Path)
	check := func(path string, typ EntryType) ArchiveEntry {
		t.Helper()
		e, ok := entryAt(a.Manifest, path)
		if !ok {
			t.Fatalf("manifest 缺少 %s", path)
		}
		if e.Type != typ {
			t.Fatalf("%s 类型应为 %s，实际 %s", path, typ, e.Type)
		}
		if _, ok := members[e.Member]; !ok {
			t.Fatalf("归档缺少成员 %s", e.Member)
		}
		return e
	}

	e := check(cfg, EntryFile)
	if e.SHA256 != shaOf("managed_projects: []\n") || members[e.Member].body != "managed_projects: []\n" || e.Mode != 0o644 {
		t.Fatalf("文件内容、sha256 或权限不符：%+v", e)
	}
	if e.Rule != RuleDecHome {
		t.Fatalf("应记录命中规则，实际 %q", e.Rule)
	}
	if !strings.HasSuffix(e.Member, strings.TrimPrefix(filepath.ToSlash(cfg), "/")) {
		t.Fatalf("成员名应保留原路径结构：%s", e.Member)
	}
	check(filepath.Dir(secret), EntryDir)
	if e := check(secret, EntryFile); e.Mode != 0o600 {
		t.Fatalf("应保留原权限 0600，实际 %o", e.Mode)
	}
	if e := check(link, EntrySymlink); e.LinkTarget != filepath.Join(root, "nowhere") {
		t.Fatalf("应记录链接目标，实际 %q", e.LinkTarget)
	}
	if e := check(alive, EntrySymlink); members[e.Member].hdr.Typeflag != tar.TypeSymlink {
		t.Fatal("指向目录的链接应按链接归档")
	}
	if _, ok := entryAt(a.Manifest, real); ok {
		t.Fatal("链接指向的内容不应被跟随归档")
	}
	e = check(rc, EntryFragment)
	if e.LinkTarget != rcReal || members[e.Member].body != "export A=1\n# Dec\nexport PATH=~/.dec/bin:$PATH\n" {
		t.Fatalf("共享文件应归档链接指向的全文并记录链接：%+v", e)
	}

	// manifest.json 在归档里，并与旁路副本一致。
	inner, ok := members[manifestMember]
	if !ok {
		t.Fatal("归档缺少 manifest.json")
	}
	side, err := os.ReadFile(filepath.Join(ArchiveDir(state), a.ID+".json"))
	if err != nil || string(side) != inner.body {
		t.Fatalf("旁路清单应与归档内 manifest 一致：%v", err)
	}
	var parsed Manifest
	if err := json.Unmarshal(side, &parsed); err != nil || len(parsed.Entries) != len(a.Manifest.Entries) {
		t.Fatalf("旁路清单解析失败：%v", err)
	}
	if _, err := VerifyArchive(a.Path); err != nil {
		t.Fatalf("落定后的归档应能重新校验通过：%v", err)
	}
	// 原文件一律不动：删除是任务 4 的事。
	for _, p := range []string{cfg, secret, link, alive, rc, rcReal} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("归档不应改动原文件 %s：%v", p, err)
		}
	}
}

// 归档目录 0700，归档与清单 0600，即使目录事先以宽权限存在（需求 4.3）。
func TestArchivePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不使用 Unix 权限位")
	}
	state, root := archiveFixture(t)
	if err := os.MkdirAll(ArchiveDir(state), 0o755); err != nil {
		t.Fatal(err)
	}
	p := put(t, filepath.Join(root, ".decs/x"), "x", 0o644)
	a, err := WriteArchive(state, []Item{{Rule: RuleDecsHome, Action: ActionDelete, Path: p}}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	mode := func(p string) os.FileMode {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode().Perm()
	}
	if m := mode(ArchiveDir(state)); m != 0o700 {
		t.Fatalf("归档目录应为 0700，实际 %o", m)
	}
	for _, f := range []string{a.Path, filepath.Join(ArchiveDir(state), a.ID+".json")} {
		if m := mode(f); m != 0o600 {
			t.Fatalf("%s 应为 0600，实际 %o", f, m)
		}
	}
}

// 任何一项读不出来就整次中止：不留半成品，原文件不动（需求 4.2）。
func TestArchiveFailureLeavesNothing(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("需要非 root 的 Unix 权限语义")
	}
	state, root := archiveFixture(t)
	ok := put(t, filepath.Join(root, ".decs/a"), "a", 0o644)
	bad := put(t, filepath.Join(root, ".decs/b"), "b", 0o000)
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })

	_, err := WriteArchive(state, []Item{{Rule: RuleDecsHome, Action: ActionDelete, Path: filepath.Dir(ok), IsDir: true}}, fixedNow)
	if err == nil {
		t.Fatal("有文件读不出来时应失败")
	}
	entries, _ := os.ReadDir(ArchiveDir(state))
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("失败后归档目录应为空，实际 %v", names)
	}
	for _, p := range []string{ok, bad} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("失败后原文件应原样保留：%s", p)
		}
	}
}

// 校验能拦住内容被篡改、成员缺失与清单外成员（需求 4.2）。
func TestVerifyDetectsTampering(t *testing.T) {
	state, root := archiveFixture(t)
	p1 := put(t, filepath.Join(root, ".decs/a"), "aaa", 0o644)
	p2 := put(t, filepath.Join(root, ".decs/b"), "bbb", 0o644)
	a, err := WriteArchive(state, []Item{{Rule: RuleDecsHome, Action: ActionDelete, Path: filepath.Dir(p1), IsDir: true}}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := json.MarshalIndent(a.Manifest, "", "  ")
	if err := verifyFile(a.Path, a.Manifest, orig); err != nil {
		t.Fatalf("原样应通过：%v", err)
	}

	mutate := func(name string, f func(m *Manifest)) {
		t.Helper()
		var m Manifest
		_ = json.Unmarshal(orig, &m)
		f(&m)
		// 用原 manifest 原文核对 manifest 成员，只考察条目层面的不一致。
		if err := verifyFile(a.Path, &m, orig); err == nil {
			t.Fatalf("%s：应校验失败", name)
		}
	}
	mutate("sha256 不符", func(m *Manifest) {
		for i := range m.Entries {
			if m.Entries[i].Path == p2 {
				m.Entries[i].SHA256 = shaOf("tampered")
			}
		}
	})
	mutate("清单多出一项", func(m *Manifest) {
		m.Entries = append(m.Entries, ArchiveEntry{Path: "/x", Member: "files/x", Type: EntryFile})
	})
	mutate("清单少一项", func(m *Manifest) {
		m.Entries = m.Entries[:len(m.Entries)-1]
	})
	if err := verifyFile(a.Path, a.Manifest, []byte("{}")); err == nil {
		t.Fatal("manifest 原文不符应校验失败")
	}

	// 截断归档：gzip 或 tar 层必须报错。
	data, _ := os.ReadFile(a.Path)
	trunc := filepath.Join(t.TempDir(), "t.tar.gz")
	_ = os.WriteFile(trunc, data[:len(data)-8], 0o600)
	if err := verifyFile(trunc, a.Manifest, orig); err == nil {
		t.Fatal("截断的归档应校验失败")
	}
}

// 重复与被包含的命中只归档一次；同路径整删优先；拒绝归档代理状态目录。
func TestNormalizeItems(t *testing.T) {
	state, root := archiveFixture(t)
	dir := filepath.Join(root, ".claude/skills/dec-x")
	inner := put(t, filepath.Join(dir, "SKILL.md"), "x", 0o644)
	mcp := put(t, filepath.Join(dir, "mcp.json"), "{}", 0o644)

	a, err := WriteArchive(state, []Item{
		{Rule: RuleProjectHost, Action: ActionDelete, Path: dir},
		{Rule: RuleUserHostItems, Action: ActionDelete, Path: dir},
		{Rule: RuleDecHome, Action: ActionDelete, Path: inner},
		{Rule: RuleUserMCP, Action: ActionStrip, Path: mcp, Entries: []string{"dec"}},
	}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Manifest.Items) != 1 {
		t.Fatalf("应合并为 1 项，实际 %+v", a.Manifest.Items)
	}
	count := 0
	for _, e := range a.Manifest.Entries {
		if e.Path == inner {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("同一文件应只归档一次，实际 %d", count)
	}

	if _, err := WriteArchive(state, []Item{{Rule: RuleDecHome, Action: ActionDelete, Path: state}}, fixedNow); err == nil {
		t.Fatal("应拒绝归档代理状态目录")
	}
	if _, err := WriteArchive(state, []Item{{Rule: RuleSSH, Action: ActionList, Path: inner}}, fixedNow); err == nil {
		t.Fatal("没有可归档的条目时应报错，而不是产生空归档（需求 3.3）")
	}
	if _, err := WriteArchive(state, []Item{{Rule: RuleDecHome, Action: ActionDelete, Path: "relative/path"}}, fixedNow); err == nil {
		t.Fatal("应拒绝相对路径")
	}
}

// 同一秒内多次执行得到不同 id，不覆盖已有归档。
func TestArchiveIDsDoNotCollide(t *testing.T) {
	state, root := archiveFixture(t)
	var ids []string
	for i := 0; i < 3; i++ {
		p := put(t, filepath.Join(root, ".decs", string(rune('a'+i))), "x", 0o644)
		a, err := WriteArchive(state, []Item{{Rule: RuleDecsHome, Action: ActionDelete, Path: p}}, fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	if ids[0] == ids[1] || ids[1] == ids[2] || ids[2] != "20261028T093000Z-3" {
		t.Fatalf("id 应互不相同，实际 %v", ids)
	}
	for _, id := range ids {
		if _, err := LoadManifest(state, id); err != nil {
			t.Fatalf("每份归档都应有清单：%s %v", id, err)
		}
	}
}

// socket 这类特殊文件只记元数据，不让整次归档失败（旧版代理目录里可能留有 socket）。
func TestArchiveSpecialFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("无 Unix socket")
	}
	// macOS 的 socket 路径上限约 104 字节，t.TempDir() 太长。
	dir, err := os.MkdirTemp("/tmp", "dl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("无法创建 socket：", err)
	}
	defer ln.Close()
	put(t, filepath.Join(dir, "f"), "f", 0o644)

	state := filepath.Join(t.TempDir(), "state")
	a, err := WriteArchive(state, []Item{{Rule: RuleDecHome, Action: ActionDelete, Path: dir, IsDir: true}}, fixedNow)
	if err != nil {
		t.Fatalf("特殊文件不应让归档失败：%v", err)
	}
	e, ok := entryAt(a.Manifest, sock)
	if !ok || e.Type != EntryOther || e.Member != "" {
		t.Fatalf("socket 应只记元数据：%+v", e)
	}
}

// 共享文件不是普通文件时拒绝（不能“只删条目”）。
func TestFragmentMustBeRegular(t *testing.T) {
	state, root := archiveFixture(t)
	d := filepath.Join(root, ".zshrc")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteArchive(state, []Item{{Rule: RuleShellRC, Action: ActionStrip, Path: d}}, fixedNow); err == nil {
		t.Fatal("目录不能按共享文件处理")
	}
}
