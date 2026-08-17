package secrets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAndLoadSSHKeyMaterial(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	mat, err := GenerateSSHKeyMaterial("dec-test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mat.PrivateKey, "PRIVATE KEY") {
		t.Fatalf("private key missing: %q", mat.PrivateKey[:min(40, len(mat.PrivateKey))])
	}
	if !strings.HasPrefix(strings.TrimSpace(mat.PublicKey), "ssh-") {
		t.Fatalf("public key = %q", mat.PublicKey)
	}
	if !strings.HasPrefix(mat.KeyFingerprint, "SHA256:") {
		t.Fatalf("fingerprint = %q", mat.KeyFingerprint)
	}

	dir := t.TempDir()
	priv := filepath.Join(dir, "id_ed25519")
	// Windows 上 os.WriteFile(0600) 仍会继承 TempDir 的组 ACL（如 CodexSandboxUsers），
	// OpenSSH 会拒绝读私钥。生产落地走 writeSecureFile，测试必须对齐。
	if err := writeSecureFile(priv, []byte(mat.PrivateKey), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSSHKeyMaterialFromPrivatePath(priv)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.KeyFingerprint != mat.KeyFingerprint {
		t.Fatalf("fingerprint mismatch: %q vs %q", loaded.KeyFingerprint, mat.KeyFingerprint)
	}
}

// 回归：源文件带继承组 ACL 时，Load 不得改源文件，而是用安全临时副本喂给 ssh-keygen。
func TestLoadSSHKeyMaterialFromPrivatePath_ToleratesInheritedACL(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	mat, err := GenerateSSHKeyMaterial("dec-acl")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	priv := filepath.Join(dir, "loose_acl_key")
	// 故意不用 writeSecureFile：模拟用户从 Temp / 解压目录选出的私钥。
	if err := os.WriteFile(priv, []byte(mat.PrivateKey), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSSHKeyMaterialFromPrivatePath(priv)
	if err != nil {
		t.Fatalf("松散 ACL 源文件应仍可加载: %v", err)
	}
	if loaded.KeyFingerprint != mat.KeyFingerprint {
		t.Fatalf("fingerprint mismatch: %q vs %q", loaded.KeyFingerprint, mat.KeyFingerprint)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
