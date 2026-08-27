package secrets

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func useTempSSHHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	return home
}

func TestParseSSHHostSpec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw        string
		wantHost   string
		wantPort   int
		wantCanon  string
		wantErrSub string
	}{
		{"vikunja.example.com", "vikunja.example.com", 0, "vikunja.example.com", ""},
		{"21.214.34.79:36000", "21.214.34.79", 36000, "21.214.34.79:36000", ""},
		{"osgamecore.devcloud.woa.com:36000", "osgamecore.devcloud.woa.com", 36000, "osgamecore.devcloud.woa.com:36000", ""},
		{"update.devcloud.woa.com:36000", "update.devcloud.woa.com", 36000, "update.devcloud.woa.com:36000", ""},
		{"host:0", "", 0, "", "端口非法"},
		{"host:65536", "", 0, "", "端口非法"},
		{"host:", "", 0, "", "端口非法"},
		{"a:1:2", "", 0, "", "非法"},
		{"host.com ProxyCommand=evil", "", 0, "", "非法字符"},
		{":36000", "", 0, "", "非法"},
	}
	for _, tc := range cases {
		host, port, canon, err := parseSSHHostSpec(tc.raw)
		if tc.wantErrSub != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Fatalf("parseSSHHostSpec(%q) err = %v, want 含 %q", tc.raw, err, tc.wantErrSub)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseSSHHostSpec(%q) = %v", tc.raw, err)
		}
		if host != tc.wantHost || port != tc.wantPort || canon != tc.wantCanon {
			t.Fatalf("parseSSHHostSpec(%q) = (%q,%d,%q), want (%q,%d,%q)",
				tc.raw, host, port, canon, tc.wantHost, tc.wantPort, tc.wantCanon)
		}
	}
}

func TestPrepareAndWriteSSHKeyLandings_HostPort(t *testing.T) {
	home := useTempSSHHome(t)
	sshDir := filepath.Join(home, ".ssh")

	landings, err := PrepareSSHKeyLandings("woa", []SSHKeyItem{{
		Name: ".sshkey/devcloud",
		Hosts: []string{
			"21.214.34.79:36000",
			"osgamecore.devcloud.woa.com:36000",
			"update.devcloud.woa.com:36000",
		},
		PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nPRIV\n-----END OPENSSH PRIVATE KEY-----\n",
		PublicKey:  "ssh-ed25519 AAAA devcloud@dec\n",
	}})
	if err != nil {
		t.Fatalf("PrepareSSHKeyLandings() = %v", err)
	}
	if len(landings) != 1 || len(landings[0].Hosts) != 3 {
		t.Fatalf("landing Hosts = %#v", landings)
	}
	for i, want := range []string{
		"21.214.34.79:36000",
		"osgamecore.devcloud.woa.com:36000",
		"update.devcloud.woa.com:36000",
	} {
		if landings[0].Hosts[i] != want {
			t.Fatalf("Hosts[%d] = %q, want %q", i, landings[0].Hosts[i], want)
		}
	}
	if err := WriteSSHKeyLandings(landings); err != nil {
		t.Fatalf("WriteSSHKeyLandings() = %v", err)
	}

	content := mustReadSSHFile(t, filepath.Join(sshDir, "config.d", "dec.conf"))
	if !strings.Contains(content, "21.214.34.79") ||
		!strings.Contains(content, "osgamecore.devcloud.woa.com") ||
		!strings.Contains(content, "update.devcloud.woa.com") {
		t.Fatalf("应写入三个 Host 模式:\n%s", content)
	}
	if !strings.Contains(content, "Port 36000") {
		t.Fatalf("应写入 Port 36000:\n%s", content)
	}
	if strings.Count(content, "Port 36000") != 1 {
		t.Fatalf("同 IdentityFile+Port 应合并为一个 Port 行:\n%s", content)
	}
	if !strings.Contains(content, "dec_woa_devcloud") {
		t.Fatalf("应写入 IdentityFile:\n%s", content)
	}
	mainCfg := mustReadSSHFile(t, filepath.Join(sshDir, "config"))
	if !strings.HasPrefix(strings.TrimLeft(mainCfg, "\r\n"), sshManagedIncludeLine) {
		t.Fatalf("主 config 应以 Include 置顶:\n%s", mainCfg)
	}
}

func TestPrepareSSHKeyLandings_HostPortConflictSameHost(t *testing.T) {
	useTempSSHHome(t)
	_, err := PrepareSSHKeyLandings("woa", []SSHKeyItem{{
		Name: ".sshkey/a", Hosts: []string{"same.example.com:36000"}, PrivateKey: "key-a\n",
	}, {
		Name: ".sshkey/b", Hosts: []string{"same.example.com:22022"}, PrivateKey: "key-b\n",
	}})
	if err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("同 Host 不同 Port 仍应冲突: %v", err)
	}
}

func TestWriteSSHKeyLandings_DifferentPortsSplitBlocks(t *testing.T) {
	home := useTempSSHHome(t)
	sshDir := filepath.Join(home, ".ssh")

	landings, err := PrepareSSHKeyLandings("woa", []SSHKeyItem{{
		Name: ".sshkey/devcloud",
		Hosts: []string{
			"a.example.com:36000",
			"b.example.com:22022",
		},
		PrivateKey: "priv\n", PublicKey: "pub\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(landings); err != nil {
		t.Fatal(err)
	}
	content := mustReadSSHFile(t, filepath.Join(sshDir, "config.d", "dec.conf"))
	if strings.Count(content, "Port 36000") != 1 || strings.Count(content, "Port 22022") != 1 {
		t.Fatalf("不同端口应拆成多个 Host 块:\n%s", content)
	}
}

func TestWriteSSHKeyLandings_PreservesOtherPortOnUpsert(t *testing.T) {
	home := useTempSSHHome(t)
	sshDir := filepath.Join(home, ".ssh")

	other, err := PrepareSSHKeyLandings("other", []SSHKeyItem{{
		Name: ".sshkey/key", Hosts: []string{"other.example.com:36000"},
		PrivateKey: "o\n", PublicKey: "op\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(other); err != nil {
		t.Fatal(err)
	}

	mine, err := PrepareSSHKeyLandings("woa", []SSHKeyItem{{
		Name: ".sshkey/devcloud", Hosts: []string{"woa.example.com"},
		PrivateKey: "w\n", PublicKey: "wp\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(mine); err != nil {
		t.Fatal(err)
	}

	content := mustReadSSHFile(t, filepath.Join(sshDir, "config.d", "dec.conf"))
	if !strings.Contains(content, "other.example.com") || !strings.Contains(content, "Port 36000") {
		t.Fatalf("upsert 后应保留其他 IdentityFile 的 Port:\n%s", content)
	}
	if !strings.Contains(content, "woa.example.com") {
		t.Fatalf("应写入本次 host:\n%s", content)
	}
	// mine 无端口：其 Host 块不应误带 Port（Port 36000 只属于 other）
	entries := parseManagedEntries(content)
	var woaPort, otherPort int
	for _, e := range entries {
		switch e.Host {
		case "woa.example.com":
			woaPort = e.Port
		case "other.example.com":
			otherPort = e.Port
		}
	}
	if woaPort != 0 {
		t.Fatalf("woa host Port = %d, want 0", woaPort)
	}
	if otherPort != 36000 {
		t.Fatalf("other host Port = %d, want 36000", otherPort)
	}
}

func TestSSHKeyFileName_RejectsTraversal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		bundle, name string
	}{
		{"../evil", "deploy"},
		{"vikunja", "a/b"},
		{"vikunja", ".."},
		{"", "deploy"},
		{"vikunja", ""},
		{"vikunja", "bad name"},
	}
	for _, tc := range cases {
		if _, err := SSHKeyFileName(tc.bundle, tc.name); err == nil {
			t.Fatalf("SSHKeyFileName(%q, %q) 应失败", tc.bundle, tc.name)
		}
	}
}

func TestPrepareSSHKeyLandings_RejectsHostInjectionAndConflicts(t *testing.T) {
	useTempSSHHome(t)

	_, err := PrepareSSHKeyLandings("vikunja", []SSHKeyItem{{
		Name: ".sshkey/deploy", Hosts: []string{"host.com ProxyCommand=evil"}, PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----",
	}})
	if err == nil {
		t.Fatal("含空格的 host 应被拒绝")
	}

	_, err = PrepareSSHKeyLandings("vikunja", []SSHKeyItem{
		{Name: ".sshkey/a", Hosts: []string{"same.example.com"}, PrivateKey: "key-a\n"},
		{Name: ".sshkey/b", Hosts: []string{"same.example.com"}, PrivateKey: "key-b\n"},
	})
	if err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("同批 host 冲突应报错: %v", err)
	}
}

func TestWriteSSHKeyLandings_PreservesUserConfigAndOtherProjectEntries(t *testing.T) {
	home := useTempSSHHome(t)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	userConfig := "Host personal\n  IdentityFile ~/.ssh/id_ed25519\n\n"
	otherManaged := sshManagedBegin + "\n" +
		"Host other.example.com\n" +
		"  IdentityFile " + filepath.Join(sshDir, "dec_other_key") + "\n" +
		sshManagedEnd + "\n"
	// 模拟旧版：managed 内嵌在主 config 尾部
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(userConfig+otherManaged), 0600); err != nil {
		t.Fatal(err)
	}

	landings, err := PrepareSSHKeyLandings("vikunja", []SSHKeyItem{{
		Name:       ".sshkey/deploy",
		Hosts:      []string{"vikunja.example.com"},
		PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nPRIV\n-----END OPENSSH PRIVATE KEY-----\n",
		PublicKey:  "ssh-ed25519 AAAA deploy@dec\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(landings); err != nil {
		t.Fatalf("WriteSSHKeyLandings() = %v", err)
	}

	priv := filepath.Join(sshDir, "dec_vikunja_deploy")
	pub := priv + ".pub"
	if _, err := os.Stat(priv); err != nil {
		t.Fatalf("私钥未落地: %v", err)
	}
	if _, err := os.Stat(pub); err != nil {
		t.Fatalf("公钥未落地: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(priv)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("私钥权限 = %o, want 0600", info.Mode().Perm())
		}
	}

	mainCfg := mustReadSSHFile(t, filepath.Join(sshDir, "config"))
	if !strings.HasPrefix(strings.TrimLeft(mainCfg, "\r\n"), sshManagedIncludeLine) {
		t.Fatalf("主 config 应以 Include 置顶:\n%s", mainCfg)
	}
	if !strings.Contains(mainCfg, "Host personal") {
		t.Fatalf("应保留主 config 用户配置:\n%s", mainCfg)
	}
	if strings.Contains(mainCfg, sshManagedBegin) || strings.Contains(mainCfg, "vikunja.example.com") {
		t.Fatalf("managed Host 不应再内嵌主 config:\n%s", mainCfg)
	}

	managed := mustReadSSHFile(t, filepath.Join(sshDir, "config.d", "dec.conf"))
	if !strings.Contains(managed, "dec_other_key") {
		t.Fatalf("应迁移并保留其他项目 managed 条目:\n%s", managed)
	}
	if !strings.Contains(managed, "vikunja.example.com") || !strings.Contains(managed, "dec_vikunja_deploy") {
		t.Fatalf("应写入本次 SSH Key 条目到 config.d:\n%s", managed)
	}

	// 再次 pull 同 key 但换 host：应替换本 IdentityFile，仍保留 other。
	landings2, err := PrepareSSHKeyLandings("vikunja", []SSHKeyItem{{
		Name:       ".sshkey/deploy",
		Hosts:      []string{"vikunja-new.example.com"},
		PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nPRIV2\n-----END OPENSSH PRIVATE KEY-----\n",
		PublicKey:  "ssh-ed25519 BBBB deploy@dec\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(landings2); err != nil {
		t.Fatal(err)
	}
	managed = mustReadSSHFile(t, filepath.Join(sshDir, "config.d", "dec.conf"))
	if strings.Contains(managed, "vikunja.example.com") {
		t.Fatalf("旧 host 应被替换:\n%s", managed)
	}
	if !strings.Contains(managed, "vikunja-new.example.com") || !strings.Contains(managed, "dec_other_key") {
		t.Fatalf("新 host + 其他项目条目应存在:\n%s", managed)
	}
	mainCfg = mustReadSSHFile(t, filepath.Join(sshDir, "config"))
	if !strings.HasPrefix(strings.TrimLeft(mainCfg, "\r\n"), sshManagedIncludeLine) {
		t.Fatalf("换 host 后 Include 仍应置顶:\n%s", mainCfg)
	}
}

func mustReadSSHFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}
	return string(raw)
}

func TestRemoveSSHKeyLanding_RemovesFilesAndManagedEntries(t *testing.T) {
	home := useTempSSHHome(t)
	sshDir := filepath.Join(home, ".ssh")

	landings, err := PrepareSSHKeyLandings("vikunja", []SSHKeyItem{{
		Name: ".sshkey/deploy", Hosts: []string{"vikunja.example.com"},
		PrivateKey: "priv\n", PublicKey: "pub\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(landings); err != nil {
		t.Fatal(err)
	}
	// 再写入另一项目条目
	other, err := PrepareSSHKeyLandings("other", []SSHKeyItem{{
		Name: ".sshkey/key", Hosts: []string{"other.example.com"},
		PrivateKey: "o\n", PublicKey: "op\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(other); err != nil {
		t.Fatal(err)
	}

	if err := RemoveSSHKeyLanding("vikunja", "deploy"); err != nil {
		t.Fatalf("RemoveSSHKeyLanding() = %v", err)
	}
	if _, err := os.Stat(filepath.Join(sshDir, "dec_vikunja_deploy")); !os.IsNotExist(err) {
		t.Fatal("私钥应已删除")
	}
	managed := mustReadSSHFile(t, filepath.Join(sshDir, "config.d", "dec.conf"))
	if strings.Contains(managed, "vikunja.example.com") || strings.Contains(managed, "dec_vikunja_deploy") {
		t.Fatalf("managed 中不应再有已删 key:\n%s", managed)
	}
	if !strings.Contains(managed, "other.example.com") {
		t.Fatalf("其他项目条目应保留:\n%s", managed)
	}
	mainCfg := mustReadSSHFile(t, filepath.Join(sshDir, "config"))
	if !strings.HasPrefix(strings.TrimLeft(mainCfg, "\r\n"), sshManagedIncludeLine) {
		t.Fatalf("仍有其他 managed 时 Include 应保留:\n%s", mainCfg)
	}
}

func TestParseSSHHostsNotes(t *testing.T) {
	t.Parallel()
	hosts := parseSSHHostsNotes("a.example.com\n\n# comment\nb.example.com\na.example.com\n")
	if len(hosts) != 2 || hosts[0] != "a.example.com" || hosts[1] != "b.example.com" {
		t.Fatalf("hosts = %#v", hosts)
	}
	if empty := parseSSHHostsNotes(""); len(empty) != 0 {
		t.Fatalf("空 Notes 应得到空 hosts, got %#v", empty)
	}
	if blank := parseSSHHostsNotes("  \n# only comment\n\n"); len(blank) != 0 {
		t.Fatalf("仅空白/注释应得到空 hosts, got %#v", blank)
	}
}

func TestFormatSSHHostsNotes(t *testing.T) {
	t.Parallel()
	if got := formatSSHHostsNotes(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	if got := formatSSHHostsNotes([]string{" a.example.com ", "", "#x", "a.example.com", "b.example.com"}); got != "a.example.com\nb.example.com\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPrepareAndWriteSSHKeyLandings_EmptyHosts_WritesKeysOnly(t *testing.T) {
	home := useTempSSHHome(t)
	sshDir := filepath.Join(home, ".ssh")

	// 先写入带 host 的 key，再以空 hosts 重 pull：应保留密钥文件、清掉本 IdentityFile 的 Host 条目。
	withHost, err := PrepareSSHKeyLandings("vikunja", []SSHKeyItem{{
		Name: ".sshkey/deploy", Hosts: []string{"vikunja.example.com"},
		PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nPRIV\n-----END OPENSSH PRIVATE KEY-----\n",
		PublicKey:  "ssh-ed25519 AAAA deploy@dec\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(withHost); err != nil {
		t.Fatal(err)
	}

	other, err := PrepareSSHKeyLandings("other", []SSHKeyItem{{
		Name: ".sshkey/key", Hosts: []string{"other.example.com"},
		PrivateKey: "o\n", PublicKey: "op\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(other); err != nil {
		t.Fatal(err)
	}

	emptyHosts, err := PrepareSSHKeyLandings("vikunja", []SSHKeyItem{{
		Name:       ".sshkey/deploy",
		Hosts:      nil,
		PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nPRIV2\n-----END OPENSSH PRIVATE KEY-----\n",
		PublicKey:  "ssh-ed25519 BBBB deploy@dec\n",
	}})
	if err != nil {
		t.Fatalf("空 hosts 应允许 Prepare: %v", err)
	}
	if len(emptyHosts) != 1 || len(emptyHosts[0].Hosts) != 0 {
		t.Fatalf("landing Hosts 应为空: %#v", emptyHosts)
	}
	if err := WriteSSHKeyLandings(emptyHosts); err != nil {
		t.Fatalf("空 hosts WriteSSHKeyLandings 应成功: %v", err)
	}

	priv := filepath.Join(sshDir, "dec_vikunja_deploy")
	pub := priv + ".pub"
	if _, err := os.Stat(priv); err != nil {
		t.Fatalf("私钥应落地: %v", err)
	}
	if _, err := os.Stat(pub); err != nil {
		t.Fatalf("公钥应落地: %v", err)
	}
	rawPriv, err := os.ReadFile(priv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawPriv), "PRIV2") {
		t.Fatalf("私钥内容应已更新: %q", rawPriv)
	}

	content := mustReadSSHFile(t, filepath.Join(sshDir, "config.d", "dec.conf"))
	if strings.Contains(content, "vikunja.example.com") || strings.Contains(content, "dec_vikunja_deploy") {
		t.Fatalf("空 hosts 不应写入本 key 的 Host 条目:\n%s", content)
	}
	if strings.Contains(content, "Host *") {
		t.Fatalf("空 hosts 不得使用 Host *:\n%s", content)
	}
	if !strings.Contains(content, "other.example.com") || !strings.Contains(content, "dec_other_key") {
		t.Fatalf("其他项目 Host 条目应保留:\n%s", content)
	}
}

func TestWriteSSHKeyLandings_EmptyHostsOnly_NoHostBlock(t *testing.T) {
	home := useTempSSHHome(t)
	sshDir := filepath.Join(home, ".ssh")

	landings, err := PrepareSSHKeyLandings("vikunja", []SSHKeyItem{{
		Name: ".sshkey/deploy", Hosts: nil,
		PrivateKey: "priv\n", PublicKey: "pub\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(landings); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sshDir, "dec_vikunja_deploy")); err != nil {
		t.Fatalf("私钥应落地: %v", err)
	}
	cfgPath := filepath.Join(sshDir, "config")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		// 允许不存在或为空；若存在则不得含 Include / DEC Host。
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	} else {
		content := string(raw)
		if strings.Contains(content, "Host ") || strings.Contains(content, sshManagedIncludeLine) || strings.Contains(content, sshManagedBegin) {
			t.Fatalf("仅空 hosts 时不应写入 Host / Include:\n%s", content)
		}
	}
	if _, err := os.Stat(filepath.Join(sshDir, "config.d", "dec.conf")); !os.IsNotExist(err) {
		t.Fatalf("仅空 hosts 时不应留下 config.d/dec.conf: %v", err)
	}
}

func TestProjectSSHKeyLandingIsScopedByGitDirAndRevocable(t *testing.T) {
	home := useTempSSHHome(t)
	projectRoot := filepath.Join(t.TempDir(), "project")
	otherRoot := filepath.Join(t.TempDir(), "other")
	for _, root := range []string{projectRoot, otherRoot} {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, out)
		}
	}
	landings, err := PrepareSSHKeyLandings("my-app", []SSHKeyItem{{
		Name: ".sshkey/deploy", Hosts: []string{"git.example.com:2222"},
		PrivateKey: "private\n", PublicKey: "public\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteProjectSSHKeyLandings(projectRoot, landings); err != nil {
		t.Fatal(err)
	}
	paths, err := ProjectCredentialScopePaths(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	sshFragment := mustReadSSHFile(t, paths.SSHFragment)
	if !strings.Contains(sshFragment, "Host git.example.com") ||
		!strings.Contains(sshFragment, "IdentityFile none") ||
		!strings.Contains(sshFragment, "Port 2222") {
		t.Fatalf("project SSH fragment 不完整:\n%s", sshFragment)
	}
	mainPath := filepath.Join(home, ".ssh", "config")
	if raw, readErr := os.ReadFile(mainPath); readErr == nil && strings.Contains(string(raw), "git.example.com") {
		t.Fatalf("project Host 不得写入全局 SSH config:\n%s", raw)
	}
	scoped, err := exec.Command("git", "-C", projectRoot, "config", "--get", "core.sshCommand").Output()
	if err != nil || !strings.Contains(string(scoped), filepath.ToSlash(paths.SSHFragment)) {
		t.Fatalf("project 应命中 sshCommand, err=%v out=%q", err, scoped)
	}
	if out, err := exec.Command("git", "-C", otherRoot, "config", "--get", "core.sshCommand").CombinedOutput(); err == nil {
		t.Fatalf("其它仓库不应命中 project sshCommand: %q", out)
	}
	if ok, err := InspectProjectSSHKeyLanding(projectRoot, "my-app", "deploy"); err != nil || !ok {
		t.Fatalf("InspectProjectSSHKeyLanding = %v, %v", ok, err)
	}
	if err := RemoveProjectSSHKeyLanding(projectRoot, "my-app", "deploy"); err != nil {
		t.Fatal(err)
	}
	if ok, err := InspectProjectSSHKeyLanding(projectRoot, "my-app", "deploy"); err != nil || ok {
		t.Fatalf("revoke 后 Inspect = %v, %v", ok, err)
	}
	if out, err := exec.Command("git", "-C", projectRoot, "config", "--get", "core.sshCommand").CombinedOutput(); err == nil {
		t.Fatalf("revoke 后不应残留 sshCommand: %q", out)
	}
}

func TestProjectSSHKeyLandingRejectsExistingHostConflict(t *testing.T) {
	useTempSSHHome(t)
	projectRoot := t.TempDir()
	first, err := PrepareSSHKeyLandings("my-app", []SSHKeyItem{{
		Name: ".sshkey/first", Hosts: []string{"same.example.com"}, PrivateKey: "first\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteProjectSSHKeyLandings(projectRoot, first); err != nil {
		t.Fatal(err)
	}
	second, err := PrepareSSHKeyLandings("my-app", []SSHKeyItem{{
		Name: ".sshkey/second", Hosts: []string{"same.example.com:2222"}, PrivateKey: "second\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = WriteProjectSSHKeyLandings(projectRoot, second)
	if err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("已有 project Host 应参与冲突检测: %v", err)
	}
}
