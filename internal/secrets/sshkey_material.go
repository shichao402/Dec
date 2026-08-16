package secrets

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SSHKeyMaterial 是登记前已就绪的密钥素材（不含 Hosts）。
type SSHKeyMaterial struct {
	PrivateKey     string
	PublicKey      string
	KeyFingerprint string
}

// GenerateSSHKeyMaterial 本机生成无口令 ed25519 密钥（ssh-keygen）。
func GenerateSSHKeyMaterial(comment string) (SSHKeyMaterial, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = "dec"
	}
	dir, err := os.MkdirTemp("", "dec-sshkey-*")
	if err != nil {
		return SSHKeyMaterial{}, err
	}
	defer os.RemoveAll(dir)

	privPath := filepath.Join(dir, "id_ed25519")
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", comment, "-f", privPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return SSHKeyMaterial{}, fmt.Errorf("ssh-keygen 生成失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return LoadSSHKeyMaterialFromPrivatePath(privPath)
}

// LoadSSHKeyMaterialFromPrivatePath 从已有私钥文件派生公钥与 fingerprint。
func LoadSSHKeyMaterialFromPrivatePath(privPath string) (SSHKeyMaterial, error) {
	privPath = strings.TrimSpace(privPath)
	if privPath == "" {
		return SSHKeyMaterial{}, fmt.Errorf("私钥路径不能为空")
	}
	info, err := os.Stat(privPath)
	if err != nil {
		if os.IsNotExist(err) {
			return SSHKeyMaterial{}, fmt.Errorf("私钥文件不存在: %s", privPath)
		}
		return SSHKeyMaterial{}, err
	}
	if info.IsDir() {
		return SSHKeyMaterial{}, fmt.Errorf("%s 是目录", privPath)
	}
	privBytes, err := os.ReadFile(privPath)
	if err != nil {
		return SSHKeyMaterial{}, fmt.Errorf("读取私钥失败: %w", err)
	}
	priv := string(privBytes)
	if err := validateSSHKeyMaterial(priv); err != nil {
		return SSHKeyMaterial{}, fmt.Errorf("私钥格式无效")
	}

	pubOut, err := exec.Command("ssh-keygen", "-y", "-f", privPath).CombinedOutput()
	if err != nil {
		return SSHKeyMaterial{}, fmt.Errorf("从私钥派生公钥失败: %w (%s)", err, strings.TrimSpace(string(pubOut)))
	}
	pub := strings.TrimSpace(string(pubOut))
	if pub == "" {
		return SSHKeyMaterial{}, fmt.Errorf("派生公钥为空")
	}

	fpOut, err := exec.Command("ssh-keygen", "-lf", privPath).CombinedOutput()
	if err != nil {
		return SSHKeyMaterial{}, fmt.Errorf("计算 fingerprint 失败: %w (%s)", err, strings.TrimSpace(string(fpOut)))
	}
	fp := parseSSHKeygenFingerprint(string(fpOut))
	if fp == "" {
		return SSHKeyMaterial{}, fmt.Errorf("无法解析 fingerprint: %q", strings.TrimSpace(string(fpOut)))
	}
	return SSHKeyMaterial{
		PrivateKey:     priv,
		PublicKey:      pub + "\n",
		KeyFingerprint: fp,
	}, nil
}

// parseSSHKeygenFingerprint 从 `ssh-keygen -lf` 输出取 SHA256:… 段。
// 典型行：`256 SHA256:abcdef… comment (ED25519)`
func parseSSHKeygenFingerprint(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	for _, f := range fields {
		if strings.HasPrefix(f, "SHA256:") || strings.HasPrefix(f, "MD5:") {
			return f
		}
	}
	return ""
}
