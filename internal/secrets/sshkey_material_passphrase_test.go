package secrets

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/sysproc"
)

// Bug#1 回归：带口令私钥在无 TTY 的 dec-server 环境下，ssh-keygen -y 探测必须
// 快速失败（空 ASKPASS + 超时兜底），而不是挂死等待 stdin。错误必须携带
// "派生公钥失败"标记，供上层 loadSSHKeyMaterialInteractive 转入口令弹窗流程。
func TestLoadSSHKeyMaterialFromPrivatePath_PassphraseFailsFast(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	// 1. 生成带口令私钥。
	dir := t.TempDir()
	encPath := filepath.Join(dir, "id_enc")
	cmd := sysproc.Command("ssh-keygen", "-t", "ed25519",
		"-N", "secret-passphrase-123", "-f", encPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("生成带口令私钥失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	// 2. 非交互加载必须在有限时间内失败（超时值远小于 sshProbeTimeout 的 10s，
	// 只为证明"不挂死"；实际快路径应在 1s 内返回）。
	done := make(chan error, 1)
	go func() {
		_, err := LoadSSHKeyMaterialFromPrivatePath(encPath)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("带口令私钥的无口令探测应当失败")
		}
		if !strings.Contains(err.Error(), "派生公钥失败") {
			t.Fatalf("错误应带\"派生公钥失败\"标记供上层识别，实际: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("探测挂死超过 30s：Bug#1 未修复（ssh-keygen 等待 stdin 输入）")
	}
}
