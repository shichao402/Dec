package secrets

import (
	"os"
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
		Name: "devcloud",
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

	raw, err := os.ReadFile(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
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
}

func TestPrepareSSHKeyLandings_HostPortConflictSameHost(t *testing.T) {
	useTempSSHHome(t)
	_, err := PrepareSSHKeyLandings("woa", []SSHKeyItem{{
		Name: "a", Hosts: []string{"same.example.com:36000"}, PrivateKey: "key-a\n",
	}, {
		Name: "b", Hosts: []string{"same.example.com:22022"}, PrivateKey: "key-b\n",
	}})
	if err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("同 Host 不同 Port 仍应冲突: %v", err)
	}
}

func TestWriteSSHKeyLandings_DifferentPortsSplitBlocks(t *testing.T) {
	home := useTempSSHHome(t)
	sshDir := filepath.Join(home, ".ssh")

	landings, err := PrepareSSHKeyLandings("woa", []SSHKeyItem{{
		Name: "devcloud",
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
	raw, err := os.ReadFile(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if strings.Count(content, "Port 36000") != 1 || strings.Count(content, "Port 22022") != 1 {
		t.Fatalf("不同端口应拆成多个 Host 块:\n%s", content)
	}
}

func TestWriteSSHKeyLandings_PreservesOtherPortOnUpsert(t *testing.T) {
	home := useTempSSHHome(t)
	sshDir := filepath.Join(home, ".ssh")

	other, err := PrepareSSHKeyLandings("other", []SSHKeyItem{{
		Name: "key", Hosts: []string{"other.example.com:36000"},
		PrivateKey: "o\n", PublicKey: "op\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(other); err != nil {
		t.Fatal(err)
	}

	mine, err := PrepareSSHKeyLandings("woa", []SSHKeyItem{{
		Name: "devcloud", Hosts: []string{"woa.example.com"},
		PrivateKey: "w\n", PublicKey: "wp\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(mine); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if !strings.Contains(content, "other.example.com") || !strings.Contains(content, "Port 36000") {
		t.Fatalf("upsert 后应保留其他 IdentityFile 的 Port:\n%s", content)
	}
	if !strings.Contains(content, "woa.example.com") {
		t.Fatalf("应写入本次 host:\n%s", content)
	}
	// mine 无端口：其 Host 块不应误带 Port（Port 36000 只属于 other）
	entries := parseManagedEntries(func() string {
		_, managed, _, _ := splitManagedBlock(content)
		return managed
	}())
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
		Name: "deploy", Hosts: []string{"host.com ProxyCommand=evil"}, PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----",
	}})
	if err == nil {
		t.Fatal("含空格的 host 应被拒绝")
	}

	_, err = PrepareSSHKeyLandings("vikunja", []SSHKeyItem{
		{Name: "a", Hosts: []string{"same.example.com"}, PrivateKey: "key-a\n"},
		{Name: "b", Hosts: []string{"same.example.com"}, PrivateKey: "key-b\n"},
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
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(userConfig+otherManaged), 0600); err != nil {
		t.Fatal(err)
	}

	landings, err := PrepareSSHKeyLandings("vikunja", []SSHKeyItem{{
		Name:       "deploy",
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

	raw, err := os.ReadFile(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if !strings.Contains(content, "Host personal") {
		t.Fatalf("应保留区块外用户配置:\n%s", content)
	}
	if !strings.Contains(content, "dec_other_key") {
		t.Fatalf("应保留其他项目 managed 条目:\n%s", content)
	}
	if !strings.Contains(content, "vikunja.example.com") || !strings.Contains(content, "dec_vikunja_deploy") {
		t.Fatalf("应写入本次 SSH Key 条目:\n%s", content)
	}

	// 再次 pull 同 key 但换 host：应替换本 IdentityFile，仍保留 other。
	landings2, err := PrepareSSHKeyLandings("vikunja", []SSHKeyItem{{
		Name:       "deploy",
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
	raw, _ = os.ReadFile(filepath.Join(sshDir, "config"))
	content = string(raw)
	if strings.Contains(content, "vikunja.example.com") {
		t.Fatalf("旧 host 应被替换:\n%s", content)
	}
	if !strings.Contains(content, "vikunja-new.example.com") || !strings.Contains(content, "dec_other_key") {
		t.Fatalf("新 host + 其他项目条目应存在:\n%s", content)
	}
}

func TestRemoveSSHKeyLanding_RemovesFilesAndManagedEntries(t *testing.T) {
	home := useTempSSHHome(t)
	sshDir := filepath.Join(home, ".ssh")

	landings, err := PrepareSSHKeyLandings("vikunja", []SSHKeyItem{{
		Name: "deploy", Hosts: []string{"vikunja.example.com"},
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
		Name: "key", Hosts: []string{"other.example.com"},
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
	raw, err := os.ReadFile(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if strings.Contains(content, "vikunja.example.com") || strings.Contains(content, "dec_vikunja_deploy") {
		t.Fatalf("managed 中不应再有已删 key:\n%s", content)
	}
	if !strings.Contains(content, "other.example.com") {
		t.Fatalf("其他项目条目应保留:\n%s", content)
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
		Name: "deploy", Hosts: []string{"vikunja.example.com"},
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
		Name: "key", Hosts: []string{"other.example.com"},
		PrivateKey: "o\n", PublicKey: "op\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHKeyLandings(other); err != nil {
		t.Fatal(err)
	}

	emptyHosts, err := PrepareSSHKeyLandings("vikunja", []SSHKeyItem{{
		Name:       "deploy",
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

	raw, err := os.ReadFile(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
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
		Name: "deploy", Hosts: nil,
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
		// 允许不存在或为空；若存在则不得含 DEC MANAGED Host。
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return
	}
	content := string(raw)
	if strings.Contains(content, "Host ") || strings.Contains(content, sshManagedBegin) {
		t.Fatalf("仅空 hosts 时不应写入 Host / managed 区块:\n%s", content)
	}
}
