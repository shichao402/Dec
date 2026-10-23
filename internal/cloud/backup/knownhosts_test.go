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
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
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

// TestAuthPinsHostKeyAlgorithm 确认认证对象显式限定了 ed25519 主机密钥算法。
//
// 这条看着琐碎，但它是 2026-10-23 生产故障的直接原因：Go 的 ssh 客户端
// 默认把 rsa-sha2-256/512 排在 ed25519 前面，GitHub 两种都提供，协商结果
// 常落到 RSA；而 known_hosts 里按 GitHub 公布的内容只写了 ed25519 条目，
// 校验随即报 "knownhosts: key mismatch"，看上去像被中间人劫持。
// contains 断言比「跑一次真实握手」稳定，且能在离线 CI 拦住回归。
func TestAuthPinsHostKeyAlgorithm(t *testing.T) {
	_, line := testPubKey(t)
	kh := writeKnownHosts(t, "[ssh.github.com]:443", line)

	// 生成一把临时私钥供 NewPublicKeys 解析。
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("生成私钥失败: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("序列化私钥失败: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "deploy_key")
	if err := os.WriteFile(keyPath, pemEncode(t, block), 0o600); err != nil {
		t.Fatalf("写私钥失败: %v", err)
	}

	p := &Pusher{Target: Target{
		Name:       "冷备",
		URL:        "ssh://git@ssh.github.com:443/owner/repo.git",
		KeyPath:    keyPath,
		KnownHosts: kh,
	}}
	auth, err := p.auth()
	if err != nil {
		t.Fatalf("构造认证失败: %v", err)
	}
	pk, ok := auth.(*gitssh.PublicKeys)
	if !ok {
		t.Fatalf("认证类型应为 *ssh.PublicKeys，实际 %T", auth)
	}
	if len(pk.HostKeyAlgorithms) != 1 || pk.HostKeyAlgorithms[0] != ssh.KeyAlgoED25519 {
		t.Errorf("主机密钥算法应限定为 [%s]，实际 %v；"+
			"不限定会协商到 RSA，与只含 ed25519 的 known_hosts 冲突而报 key mismatch",
			ssh.KeyAlgoED25519, pk.HostKeyAlgorithms)
	}
	if pk.HostKeyCallback == nil {
		t.Error("主机密钥回调为空：等于关掉了主机身份校验")
	}
}

// pemEncode 把私钥块编码成 PEM 文本。
func pemEncode(t *testing.T, block *pem.Block) []byte {
	t.Helper()
	return pem.EncodeToMemory(block)
}
