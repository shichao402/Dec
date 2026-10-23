package backup

// 部署密钥的主机密钥校验行为。
//
// 这些用例之所以存在：生产服务器到 GitHub 的 22 端口常被云厂商或出口策略
// 拦掉，剩下能用的是 ssh.github.com:443。配置与代码都必须支持这种地址，
// 否则冷备会在真机上静默失败——这里用确定性用例把行为钉住，
// 不依赖网络。

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
)

// writeKnownHosts 写出一个只含指定主机条目的 known_hosts 文件。
func writeKnownHosts(t *testing.T, hostField, keyLine string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(hostField+" "+keyLine+"\n"), 0o600); err != nil {
		t.Fatalf("写 known_hosts 失败: %v", err)
	}
	return path
}

// testPubKey 返回一个固定的测试公钥与它的 authorized_keys 文本。
func testPubKey(t *testing.T) (ssh.PublicKey, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("生成测试密钥失败: %v", err)
	}
	key, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatalf("构造公钥失败: %v", err)
	}
	line := string(ssh.MarshalAuthorizedKey(key))
	return key, line[:len(line)-1]
}

// TestKnownHostsAcceptsPortQualifiedHost 确认 [host]:port 形式的条目
// 既能匹配带方括号的地址，也能匹配裸 host:port。
//
// go-git 传的是 net.JoinHostPort 的结果（端口非 22 时是 host:port，
// 不带方括号），而 ssh-keyscan 写进 known_hosts 的是 [host]:port。
// 两者必须能对上，否则每台新机器都要手工改 known_hosts。
func TestKnownHostsAcceptsPortQualifiedHost(t *testing.T) {
	pub, line := testPubKey(t)
	path := writeKnownHosts(t, "[ssh.github.com]:443", line)

	db, err := knownhosts.NewDB(path)
	if err != nil {
		t.Fatalf("打开 known_hosts 失败: %v", err)
	}
	cb := db.HostKeyCallback()

	addr := fakeNetAddr("ssh.github.com:443")
	for _, hostname := range []string{
		"ssh.github.com:443",   // go-git 的实际形态
		"[ssh.github.com]:443", // ssh-keyscan 写入的形态
	} {
		if err := cb(hostname, addr, pub); err != nil {
			t.Errorf("%q 应匹配 known_hosts 里的 [ssh.github.com]:443，实际报错 %v", hostname, err)
		}
	}
}

// TestKnownHostsRejectsOtherHosts 确认别的地址不会被误认。
//
// 这条是安全边界：主机密钥校验是部署密钥泄露时唯一的防线，
// 放宽成「任何主机都接受」等于关掉它。
func TestKnownHostsRejectsOtherHosts(t *testing.T) {
	pub, line := testPubKey(t)
	path := writeKnownHosts(t, "[ssh.github.com]:443", line)

	db, err := knownhosts.NewDB(path)
	if err != nil {
		t.Fatalf("打开 known_hosts 失败: %v", err)
	}
	cb := db.HostKeyCallback()

	for _, hostname := range []string{
		"ssh.github.com:22", // 端口不同
		"github.com:22",     // 主机不同
		"evil.example.com:443",
	} {
		if err := cb(hostname, fakeNetAddr(hostname), pub); err == nil {
			t.Errorf("%q 不该匹配 [ssh.github.com]:443 的条目", hostname)
		}
	}
}

// fakeNetAddr 满足 net.Addr，让回调能取到远端地址。
type fakeNetAddr string

func (f fakeNetAddr) Network() string { return "tcp" }
func (f fakeNetAddr) String() string  { return string(f) }
