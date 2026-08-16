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
	if err := os.WriteFile(priv, []byte(mat.PrivateKey), 0o600); err != nil {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
