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
}
