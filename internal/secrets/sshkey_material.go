package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/sysproc"
)

// sshProbeTimeout 是无口令私钥探测（ssh-keygen -y / -lf）的单命令超时。
// 带口令私钥在无 TTY 环境下会等待输入；空 ASKPASS 已让其快速失败，此超时
// 是最后一道安全网，防止任何形式的挂死滞留临时私钥文件。
const sshProbeTimeout = 10 * time.Second

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
	cmd := sysproc.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", comment, "-f", privPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return SSHKeyMaterial{}, fmt.Errorf("ssh-keygen 生成失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return LoadSSHKeyMaterialFromPrivatePath(privPath)
}

// LoadSSHKeyMaterialFromPrivatePath 从已有私钥文件派生公钥与 fingerprint。
//
// Windows 上 OpenSSH 要求私钥 ACL 仅当前用户可读；用户从 Temp / 网盘 / 解压目录
// 选中的文件常带继承组权限。这里不改动源文件，而是写入一份 ACL 收紧的临时副本
// 再调用 ssh-keygen（与 WriteSSHKeyLandings 同用 writeSecureFile）。
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

	securePath, cleanup, err := materializeSecurePrivateKey(privBytes)
	if err != nil {
		return SSHKeyMaterial{}, err
	}
	defer cleanup()

	pubOut, err := sshProbeKeygen(securePath, "-y")
	if err != nil {
		return SSHKeyMaterial{}, fmt.Errorf("从私钥派生公钥失败: %w (%s)", err, strings.TrimSpace(string(pubOut)))
	}
	pub := strings.TrimSpace(string(pubOut))
	if pub == "" {
		return SSHKeyMaterial{}, fmt.Errorf("派生公钥为空")
	}

	fpOut, err := sshProbeKeygen(securePath, "-l")
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

// sshProbeKeygen 在超时与空 ASKPASS 保护下执行 ssh-keygen 探测命令
// （-f keypath 由本函数统一追加，调用方只传选项如 -y、-l）。
//
// Bug#1 修复：dec-server 后台进程无 TTY，ssh-keygen 遇带口令私钥时提示
// "Enter passphrase" 并等待 stdin 输入，无人应答则永不返回 → CombinedOutput
// 永不返回 → defer cleanup 不执行（私钥明文副本滞留 %TEMP%\dec-sshkey-load-*）
// → 操作锁泄漏。
//
// 双重防护：
//  1. SSH_ASKPASS_REQUIRE=force + 空 SSH_ASKPASS 脚本：强制 ssh-keygen 走
//     ASKPASS 派生口令而非等 stdin，空脚本返回空口令 → ssh-keygen 立即
//     校验失败返回 "incorrect passphrase" 错误。错误及时返回后，上层
//     loadSSHKeyMaterialInteractive 才有机会转 AskKeyPassphrase 弹窗。
//  2. sshProbeTimeout 超时包裹：任何未预期挂起兜底取消，defer cleanup
//     必然执行，私钥临时文件必然清除。
func sshProbeKeygen(securePath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sshProbeTimeout)
	defer cancel()
	cmd := sysproc.CommandContext(ctx, "ssh-keygen", append(args, "-f", securePath)...)
	// 空输出 ASKPASS：任何口令提示都立刻得到空串，ssh-keygen 快速失败。
	askpass, askpassCleanup, err := materializeEmptyAskpass()
	if err != nil {
		return nil, fmt.Errorf("准备空 ASKPASS 失败: %w", err)
	}
	defer askpassCleanup()
	cmd.Env = append(os.Environ(),
		"SSH_ASKPASS="+askpass,
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=:0",
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("ssh-keygen 探测超时（%s，可能因私钥带口令挂起）", sshProbeTimeout)
	}
	return out, err
}

// materializeEmptyAskpass 生成一个输出空串的一次性 ASKPASS 脚本（Windows 用
// .bat，unix 用 .sh），与 remote_provision 的 writeAskpassHelper 同构。ssh-keygen
// 拿到空口令后立即校验失败返回错误，而非等待 stdin 输入。
func materializeEmptyAskpass() (string, func(), error) {
	dir, err := os.MkdirTemp("", "dec-askpass-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "askpass.bat")
		body := "@echo off\r\necho.\r\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			cleanup()
			return "", nil, err
		}
		return path, cleanup, nil
	}
	path := filepath.Join(dir, "askpass.sh")
	body := "#!/bin/sh\nprintf '\\n'\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

// LoadSSHKeyMaterialWithPassphrase 从带口令的私钥文件加载材料：先复制到收紧
// ACL 的临时文件，用口令去口令导出明文私钥（PEM 写临时文件），再派生公钥与
// fingerprint。口令由调用方（凭据请求通道）提供，仅内存流转。
func LoadSSHKeyMaterialWithPassphrase(privPath, passphrase string) (SSHKeyMaterial, error) {
	privPath = strings.TrimSpace(privPath)
	passphrase = strings.TrimSpace(passphrase)
	if privPath == "" {
		return SSHKeyMaterial{}, fmt.Errorf("私钥路径不能为空")
	}
	if passphrase == "" {
		return SSHKeyMaterial{}, fmt.Errorf("私钥口令不能为空")
	}
	privBytes, err := os.ReadFile(privPath)
	if err != nil {
		return SSHKeyMaterial{}, fmt.Errorf("读取私钥失败: %w", err)
	}

	dir, err := os.MkdirTemp("", "dec-sshkey-pass-*")
	if err != nil {
		return SSHKeyMaterial{}, err
	}
	defer os.RemoveAll(dir)
	encPath := filepath.Join(dir, "id_enc")
	plainPath := filepath.Join(dir, "id_plain")
	if err := writeSecureFile(encPath, privBytes, 0o600); err != nil {
		return SSHKeyMaterial{}, fmt.Errorf("准备加密私钥临时文件失败: %w", err)
	}

	// ssh-keygen -p 原地去口令（-N "" 新空口令）。
	out, err := sysproc.Command("ssh-keygen", "-p",
		"-P", passphrase, "-N", "", "-f", encPath).CombinedOutput()
	if err != nil {
		return SSHKeyMaterial{}, fmt.Errorf("私钥口令校验失败: %w (%s)",
			err, strings.TrimSpace(string(out)))
	}
	// 去口令结果落临时文件后加载完整材料。
	if err := os.Rename(encPath, plainPath); err != nil {
		return SSHKeyMaterial{}, err
	}
	return LoadSSHKeyMaterialFromPrivatePath(plainPath)
}

// materializeSecurePrivateKey 把私钥正文落到 ACL/mode 收紧的临时文件，供 ssh-keygen 读取。
func materializeSecurePrivateKey(priv []byte) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "dec-sshkey-load-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	path = filepath.Join(dir, "id_ed25519")
	if err := writeSecureFile(path, priv, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("准备安全私钥临时文件失败: %w", err)
	}
	return path, cleanup, nil
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
