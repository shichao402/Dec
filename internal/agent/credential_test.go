package agent

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/proto"
	"github.com/shichao402/Dec/internal/sysproc"
)

// 凭据落点（.agent/credential-landing 需求 1–3）。

func sshTarget(path string) proto.Target {
	return proto.Target{Plan: proto.PlanUser, Root: proto.RootSSH, Path: path}
}

func secretTarget(path string) proto.Target {
	return proto.Target{Plan: proto.PlanProject, ProjectID: "p1", Root: proto.RootSecrets, Path: path}
}

// credEnv 造一个带已登记项目的用户目录。
func credEnv(t *testing.T) (home, project string, w *Writer) {
	t.Helper()
	home = t.TempDir()
	project = filepath.Join(home, "workspace", "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(home, []Project{{ID: "p1", Path: project}})
	return home, project, NewWriter(r, home)
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestResolveCredentialRootsRejectWrongPlane(t *testing.T) {
	home, _, w := credEnv(t)
	_ = home
	cases := []proto.Target{
		{Plan: proto.PlanProject, ProjectID: "p1", Root: proto.RootSSH, Path: "dec_a"},
		{Plan: proto.PlanUser, Root: proto.RootSecrets, Path: "a.json"},
	}
	for _, c := range cases {
		if _, err := w.resolver.Resolve(c); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%+v 应被拒绝，实际 err=%v", c, err)
		}
	}
}

func TestResolveSSHPathWhitelist(t *testing.T) {
	home, _, w := credEnv(t)
	ok := map[string]string{
		"dec_cloud_ops":     filepath.Join(home, ".ssh", "dec_cloud_ops"),
		"dec_cloud_ops.pub": filepath.Join(home, ".ssh", "dec_cloud_ops.pub"),
		"config.d/dec.conf": filepath.Join(home, ".ssh", "config.d", "dec.conf"),
		"config":            filepath.Join(home, ".ssh", "config"),
	}
	for rel, want := range ok {
		got, err := w.resolver.Resolve(sshTarget(rel))
		if err != nil || got != want {
			t.Errorf("%q: got=%q err=%v，期望 %q", rel, got, err, want)
		}
	}
	bad := []string{
		"id_rsa", "authorized_keys", "known_hosts", "dec_A", "dec_", "sub/dec_a",
		"../dec_a", "config.d/other.conf", "config.d/dec-project-x.conf", "dec_a/../../x",
	}
	for _, rel := range bad {
		if _, err := w.resolver.Resolve(sshTarget(rel)); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%q 应被拒绝，实际 err=%v", rel, err)
		}
	}
}

func TestResolveSecretsPath(t *testing.T) {
	_, project, w := credEnv(t)
	got, err := w.resolver.Resolve(secretTarget("InvestM/vendor/futu-opend/FutuOpenD.xml"))
	want := filepath.Join(project, ".secrets", "InvestM", "vendor", "futu-opend", "FutuOpenD.xml")
	if err != nil || got != want {
		t.Fatalf("got=%q err=%v，期望 %q", got, err, want)
	}
	for _, rel := range []string{"", ".", "..", "../x", "a/../../x", "/etc/passwd", ".gitignore"} {
		if _, err := w.resolver.Resolve(secretTarget(rel)); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%q 应被拒绝，实际 err=%v", rel, err)
		}
	}
	unreg := proto.Target{Plan: proto.PlanProject, ProjectID: "nope", Root: proto.RootSecrets, Path: "a"}
	if _, err := w.resolver.Resolve(unreg); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("未登记项目应被拒绝，实际 err=%v", err)
	}
}

func TestCredentialRootsNotInIDEHostTable(t *testing.T) {
	// 凭据根不能出现在 IDE 宿主表里，否则 Detect 和卸载兜底扫描会把 ~/.ssh 当 IDE 目录。
	for _, name := range HostNames() {
		if isCredentialRoot(name) {
			t.Fatalf("凭据根 %q 不应在 IDE 宿主表里", name)
		}
	}
}

func TestApplySSHPrivateKeyNormalizesAndLocksDown(t *testing.T) {
	home, _, w := credEnv(t)
	raw := "-----BEGIN OPENSSH PRIVATE KEY-----\r\nabc\r\n-----END OPENSSH PRIVATE KEY-----"
	res, err := w.Apply(proto.File{
		ID: "asset:b:key", Targets: []proto.Target{sshTarget("dec_cloud_ops")},
		Content: raw, Secret: true, Format: proto.FormatWhole,
	})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(home, ".ssh", "dec_cloud_ops")
	data, _ := os.ReadFile(p)
	want := "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n"
	if string(data) != want {
		t.Fatalf("私钥未归一：%q", data)
	}
	if res.Hash != hashOf([]byte(want)) {
		t.Fatalf("回执哈希应按归一后的字节算")
	}
	if h, _ := HashFile(p, proto.FormatWhole, ""); h != res.Hash {
		t.Fatalf("下一轮对账的哈希与回执不一致")
	}
	if runtime.GOOS != "windows" {
		if m := mode(t, p); m != 0o600 {
			t.Fatalf("私钥权限 %o，期望 600", m)
		}
		if m := mode(t, filepath.Join(home, ".ssh")); m != 0o700 {
			t.Fatalf("~/.ssh 权限 %o，期望 700", m)
		}
	}
}

func TestApplySSHPrivateKeyIgnoresSecretFlag(t *testing.T) {
	// 私钥 0600 由路径决定，不依赖云端有没有标 Secret：标漏了也不能落成 0644。
	home, _, w := credEnv(t)
	if _, err := w.Apply(proto.File{
		ID: "k", Targets: []proto.Target{sshTarget("dec_x")}, Content: "k", Format: proto.FormatWhole,
	}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if m := mode(t, filepath.Join(home, ".ssh", "dec_x")); m != 0o600 {
			t.Fatalf("私钥权限 %o，期望 600", m)
		}
	}
}

func TestApplySSHPublicAndFragment(t *testing.T) {
	home, _, w := credEnv(t)
	for _, rel := range []string{"dec_x.pub", "config.d/dec.conf"} {
		if _, err := w.Apply(proto.File{
			ID: rel, Targets: []proto.Target{sshTarget(rel)}, Content: "x\n", Format: proto.FormatWhole,
		}); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if runtime.GOOS != "windows" {
			if m := mode(t, filepath.Join(home, ".ssh", filepath.FromSlash(rel))); m != 0o644 {
				t.Fatalf("%s 权限 %o，期望 644", rel, m)
			}
		}
	}
	if runtime.GOOS != "windows" {
		if m := mode(t, filepath.Join(home, ".ssh", "config.d")); m != 0o700 {
			t.Fatalf("config.d 权限 %o，期望 700", m)
		}
	}
}

func TestSSHConfigRequiresManagedBlock(t *testing.T) {
	_, _, w := credEnv(t)
	_, err := w.Apply(proto.File{
		ID: "c", Targets: []proto.Target{sshTarget("config")}, Content: "x", Format: proto.FormatWhole,
	})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("~/.ssh/config 整文件覆盖应被拒绝，实际 err=%v", err)
	}
	_, err = w.Apply(proto.File{
		ID: "r", Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: "rules/a.md"}},
		Content: "x", Format: proto.FormatManagedBlock,
	})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("managed_block 用在别的落点应被拒绝，实际 err=%v", err)
	}
}

func TestManagedBlockInsertReplaceRemove(t *testing.T) {
	home, _, w := credEnv(t)
	p := filepath.Join(home, ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	user := "Include ~/.ssh/devcloud_config\n\nHost git.woa.com\n  User git\n"
	if err := os.WriteFile(p, []byte(user), 0o640); err != nil {
		t.Fatal(err)
	}
	apply := func(body string) *WriteResult {
		res, err := w.Apply(proto.File{
			ID: "c", Targets: []proto.Target{sshTarget("config")}, Content: body, Format: proto.FormatManagedBlock,
		})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res := apply("Include config.d/dec.conf")
	data, _ := os.ReadFile(p)
	want := proto.BlockBegin + "\nInclude config.d/dec.conf\n" + proto.BlockEnd + "\n\n" + user
	if string(data) != want {
		t.Fatalf("块应插在最前面：\n%s", data)
	}
	if runtime.GOOS != "windows" && mode(t, p) != 0o640 {
		t.Fatalf("应保留原文件权限")
	}

	// 用户在块外追加内容：哈希不变。
	if err := os.WriteFile(p, append(data, []byte("Host extra\n")...), 0o640); err != nil {
		t.Fatal(err)
	}
	if h, _ := HashFile(p, proto.FormatManagedBlock, ""); h != res.Hash {
		t.Fatalf("块外改动不应影响哈希")
	}

	// 原地替换，不移动位置。
	apply("Include config.d/dec.conf\nInclude config.d/dec-2.conf\n")
	data, _ = os.ReadFile(p)
	if !strings.HasPrefix(string(data), proto.BlockBegin+"\nInclude config.d/dec.conf\nInclude config.d/dec-2.conf\n"+proto.BlockEnd+"\n\n") {
		t.Fatalf("替换后块位置或内容不对：\n%s", data)
	}
	if strings.Count(string(data), proto.BlockBegin) != 1 {
		t.Fatalf("替换后不应出现第二个块")
	}

	// 删除只删块，回到用户原样（包括后来追加的那行）。
	if _, err := w.Remove(proto.File{ID: "c", Targets: []proto.Target{sshTarget("config")}, Format: proto.FormatManagedBlock}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(p)
	if string(data) != user+"Host extra\n" {
		t.Fatalf("删除后应只剩用户内容：%q", data)
	}

	// 只有块的文件，删除后文件仍保留。
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	apply("Include x")
	if _, err := w.Remove(proto.File{ID: "c", Targets: []proto.Target{sshTarget("config")}, Format: proto.FormatManagedBlock}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("删掉块后文件不应被删除: %v", err)
	}
}

func TestManagedBlockRejectsMarkersAndBrokenFile(t *testing.T) {
	home, _, w := credEnv(t)
	_, err := w.Apply(proto.File{
		ID: "c", Targets: []proto.Target{sshTarget("config")},
		Content: "Host a\n" + proto.BlockEnd + "\nHost evil\n", Format: proto.FormatManagedBlock,
	})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("内容夹带边界标记应被拒绝，实际 err=%v", err)
	}
	p := filepath.Join(home, ".ssh", "config")
	broken := proto.BlockBegin + "\nHost a\n"
	if err := os.WriteFile(p, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Apply(proto.File{
		ID: "c", Targets: []proto.Target{sshTarget("config")}, Content: "x", Format: proto.FormatManagedBlock,
	}); err == nil {
		t.Fatalf("只有开始标记的文件应拒绝改写")
	}
	data, _ := os.ReadFile(p)
	if string(data) != broken {
		t.Fatalf("拒绝时不应动文件")
	}
}

func TestManagedBlockFollowsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 创建软链接需要特权")
	}
	home, _, w := credEnv(t)
	real := filepath.Join(home, "dotfiles", "ssh_config")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("Host a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Apply(proto.File{
		ID: "c", Targets: []proto.Target{sshTarget("config")}, Content: "Include x", Format: proto.FormatManagedBlock,
	}); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("软链接本身不应被替换")
	}
	data, _ := os.ReadFile(real)
	if !strings.HasPrefix(string(data), proto.BlockBegin) {
		t.Fatalf("应写进链接指向的文件：%q", data)
	}
}

func TestApplyProjectSecretWritesIgnoreAnd0600(t *testing.T) {
	_, project, w := credEnv(t)
	res, err := w.Apply(proto.File{
		ID: "s", Targets: []proto.Target{secretTarget("relkit/relkit.secrets.json")},
		Content: `{"token":"x"}`, Secret: true, Format: proto.FormatWhole,
	})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(project, ".secrets", "relkit", "relkit.secrets.json")
	if data, _ := os.ReadFile(p); string(data) != `{"token":"x"}` {
		t.Fatalf("内容应逐字节一致：%q", data)
	}
	if runtime.GOOS != "windows" && mode(t, p) != 0o600 {
		t.Fatalf("项目 secret 权限应为 600")
	}
	ign, _ := os.ReadFile(filepath.Join(project, ".secrets", ".gitignore"))
	if string(ign) != secretsIgnoreBody {
		t.Fatalf(".gitignore 内容 %q", ign)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("不应有告警：%v", res.Warnings)
	}

	// 已有不同内容的 .gitignore：保留并告警。
	if err := os.WriteFile(filepath.Join(project, ".secrets", ".gitignore"), []byte("*.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = w.Apply(proto.File{
		ID: "s2", Targets: []proto.Target{secretTarget("a.txt")}, Content: "x", Format: proto.FormatWhole,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ign, _ := os.ReadFile(filepath.Join(project, ".secrets", ".gitignore")); string(ign) != "*.json\n" {
		t.Fatalf("用户的 .gitignore 不应被覆盖")
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("应有一条告警：%v", res.Warnings)
	}
}

func TestRemoveProjectSecretCleansDirsButKeepsRoot(t *testing.T) {
	_, project, w := credEnv(t)
	tgt := secretTarget("InvestM/vendor/futu-opend/FutuOpenD.xml")
	if _, err := w.Apply(proto.File{ID: "s", Targets: []proto.Target{tgt}, Content: "<x/>", Format: proto.FormatWhole}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Remove(proto.File{ID: "s", Targets: []proto.Target{tgt}, Format: proto.FormatWhole}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".secrets", "InvestM")); !os.IsNotExist(err) {
		t.Fatalf("变空的子目录应被清掉")
	}
	if _, err := os.Stat(filepath.Join(project, ".secrets", ".gitignore")); err != nil {
		t.Fatalf(".secrets/.gitignore 应保留: %v", err)
	}
}

func TestRemoveSSHKeyKeepsDirectory(t *testing.T) {
	home, _, w := credEnv(t)
	tgt := sshTarget("dec_x")
	if _, err := w.Apply(proto.File{ID: "k", Targets: []proto.Target{tgt}, Content: "k", Format: proto.FormatWhole}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Remove(proto.File{ID: "k", Targets: []proto.Target{tgt}, Format: proto.FormatWhole}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh")); err != nil {
		t.Fatalf("~/.ssh 不应被删除: %v", err)
	}
}

// git 索引检查（需求 2.5）：用真实 git 生成索引，覆盖 v2、v4 与 worktree。

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := sysproc.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestApplyProjectSecretRefusesTrackedFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	for _, ver := range []string{"2", "3", "4"} {
		t.Run("index-v"+ver, func(t *testing.T) {
			_, project, w := credEnv(t)
			gitRun(t, project, "init", "-q")
			p := filepath.Join(project, ".secrets", "app", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
			// 多放几个条目，让 v4 的前缀压缩真正生效。
			for _, n := range []string{"a.txt", "app/b.txt", ".secrets/app/a.yaml"} {
				fp := filepath.Join(project, filepath.FromSlash(n))
				_ = os.MkdirAll(filepath.Dir(fp), 0o755)
				_ = os.WriteFile(fp, []byte(n), 0o644)
			}
			gitRun(t, project, "add", "-A")
			gitRun(t, project, "update-index", "--index-version", ver)

			_, err := w.Apply(proto.File{
				ID: "s", Targets: []proto.Target{secretTarget("app/config.yaml")}, Content: "new", Format: proto.FormatWhole,
			})
			if !errors.Is(err, ErrNotAllowed) {
				t.Fatalf("被跟踪的文件应拒绝写入，实际 err=%v", err)
			}
			if data, _ := os.ReadFile(p); string(data) != "old" {
				t.Fatalf("拒绝时不应改动文件")
			}
			// 未跟踪的同目录文件照常写。
			if _, err := w.Apply(proto.File{
				ID: "s2", Targets: []proto.Target{secretTarget("app/other.yaml")}, Content: "x", Format: proto.FormatWhole,
			}); err != nil {
				t.Fatalf("未跟踪文件应可写: %v", err)
			}
		})
	}
}

func TestTrackedByGitInWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	home := t.TempDir()
	main := filepath.Join(home, "main")
	if err := os.MkdirAll(filepath.Join(main, ".secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, ".secrets", "t"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "init", "-q")
	gitRun(t, main, "add", "-A")
	gitRun(t, main, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")
	wt := filepath.Join(home, "wt")
	gitRun(t, main, "worktree", "add", "-q", wt)
	tracked, err := trackedByGit(wt, filepath.Join(wt, ".secrets", "t"))
	if err != nil || !tracked {
		t.Fatalf("worktree 里被跟踪的文件应识别出来：tracked=%v err=%v", tracked, err)
	}
}

func TestTrackedByGitRefusesUnreadableIndex(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".git", "index"), []byte("garbage-garbage-garbage-garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := trackedByGit(home, filepath.Join(home, ".secrets", "x")); err == nil {
		t.Fatalf("读不懂的索引应返回错误，由调用方拒绝写入")
	}
}

// 云端的删除指令只带绝对路径、不带 Root（render.Plan），下面两条按这个真实格式验证。
func absDelete(id, full string, format proto.Format) proto.File {
	return proto.File{ID: id, Targets: []proto.Target{{Plan: proto.PlanUser, Path: full}}, Format: format}
}

func TestRemoveAbsoluteProjectSecretCleansDirs(t *testing.T) {
	_, project, w := credEnv(t)
	tgt := secretTarget("acceptance/token.json")
	if _, err := w.Apply(proto.File{ID: "s", Targets: []proto.Target{tgt}, Content: "{}", Format: proto.FormatWhole}); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(project, ".secrets", "acceptance", "token.json")
	if _, err := w.Remove(absDelete("s", full, proto.FormatWhole)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("文件应被删除")
	}
	if _, err := os.Stat(filepath.Dir(full)); !os.IsNotExist(err) {
		t.Fatalf("变空的子目录应被清掉（需求 2.6）")
	}
	if _, err := os.Stat(filepath.Join(project, ".secrets", ".gitignore")); err != nil {
		t.Fatalf(".secrets/.gitignore 应保留: %v", err)
	}
}

func TestRemoveAbsoluteSSHConfigBlock(t *testing.T) {
	home, _, w := credEnv(t)
	p := filepath.Join(home, ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("Host mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Apply(proto.File{
		ID: "c", Targets: []proto.Target{sshTarget("config")}, Content: "Include config.d/dec.conf\n", Format: proto.FormatManagedBlock,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Remove(absDelete("c", p, proto.FormatManagedBlock)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "Host mine\n" {
		t.Fatalf("托管块应被删除，只剩用户内容：%q", data)
	}
}

func TestClassifyAbsolute(t *testing.T) {
	home, project, w := credEnv(t)
	r := w.resolver
	cases := map[string]credentialKind{
		filepath.Join(home, ".ssh", "config"):                      credSSHConfig,
		filepath.Join(home, ".ssh", "dec_a"):                       credSSHPrivate,
		filepath.Join(home, ".ssh", "dec_a.pub"):                   credSSHPublic,
		filepath.Join(home, ".ssh", "config.d", "dec.conf"):        credSSHPublic,
		filepath.Join(home, ".ssh", "known_hosts"):                 credNone,
		filepath.Join(home, ".ssh"):                                credNone,
		filepath.Join(project, ".secrets", "a", "b.json"):          credProjectSecret,
		filepath.Join(project, ".secrets"):                         credNone,
		filepath.Join(project, ".cursor", "rules", "x.mdc"):        credNone,
		filepath.Join(home, "workspace", "other", ".secrets", "x"): credNone,
	}
	for p, want := range cases {
		if got := r.classifyAbsolute(p); got != want {
			t.Errorf("classifyAbsolute(%q) = %d, want %d", p, got, want)
		}
	}
}
