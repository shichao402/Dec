package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const (
	sshManagedBegin = "# BEGIN DEC MANAGED"
	sshManagedEnd   = "# END DEC MANAGED"
)

// 逻辑名 / bundle 名：字母数字开头，允许 . _ -，禁止路径分隔与空白。
var sshSafeNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Host 模式：字母数字 / . _ - * ?，禁止空白与 SSH config 注入字符。
var sshHostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._*?-]*$`)

// sshPortRe 限制端口为无前导零的十进制（1–65535 另在数值侧校验；"0" 会被数值拒绝）。
var sshPortRe = regexp.MustCompile(`^[1-9][0-9]{0,4}$`)

// SSHKeyLanding 描述一条已校验、待写入 ~/.ssh 的 SSH Key。
type SSHKeyLanding struct {
	DecBundleName string
	Name          string
	Hosts         []string
	PrivateKey    string
	PublicKey     string
	PrivatePath   string // 绝对路径：~/.ssh/dec_<bundle>_<name>
	PublicPath    string
	IdentityFile  string // SSH config 中使用的路径（通常同 PrivatePath）
}

// SSHDir 返回用户 ~/.ssh 目录；可通过临时 HOME 隔离测试。
func SSHDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("解析用户主目录失败: %w", err)
	}
	return filepath.Join(home, ".ssh"), nil
}

// SSHKeyFileName 生成私钥文件名（不含目录）：dec_<bundle>_<name>。
func SSHKeyFileName(decBundleName, keyName string) (string, error) {
	bundle, err := validateSSHSafeName("bundle", decBundleName)
	if err != nil {
		return "", err
	}
	name, err := validateSSHSafeName("SSH Key", keyName)
	if err != nil {
		return "", err
	}
	return "dec_" + bundle + "_" + name, nil
}

func validateSSHSafeName(kind, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s 名称不能为空", kind)
	}
	if strings.ContainsAny(value, `/\`) || strings.Contains(value, "..") {
		return "", fmt.Errorf("%s 名称含非法路径字符: %q", kind, value)
	}
	if !sshSafeNameRe.MatchString(value) {
		return "", fmt.Errorf("%s 名称非法（仅允许字母数字 ._-，且须以字母数字开头）: %q", kind, value)
	}
	return value, nil
}

// parseSSHHostSpec 解析一行 host 或 host:port（port 省略时为 0，表示不写 Port）。
// 返回规范串（host 或 host:port）供 Notes 往返；冲突检测用 host 模式（不含 port）。
func parseSSHHostSpec(raw string) (host string, port int, canonical string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, "", fmt.Errorf("SSH host 不能为空")
	}
	if strings.ContainsAny(raw, " \t\r\n\"'#=\\/") {
		return "", 0, "", fmt.Errorf("SSH host 含非法字符: %q", raw)
	}

	hostPart := raw
	portPart := ""
	if i := strings.LastIndex(raw, ":"); i >= 0 {
		hostPart = raw[:i]
		portPart = raw[i+1:]
	}
	if hostPart == "" {
		return "", 0, "", fmt.Errorf("SSH host 非法: %q", raw)
	}
	if !sshHostRe.MatchString(hostPart) {
		return "", 0, "", fmt.Errorf("SSH host 非法: %q", raw)
	}
	if portPart == "" {
		if strings.Contains(raw, ":") {
			return "", 0, "", fmt.Errorf("SSH host 端口非法: %q", raw)
		}
		return hostPart, 0, hostPart, nil
	}
	if !sshPortRe.MatchString(portPart) {
		return "", 0, "", fmt.Errorf("SSH host 端口非法: %q", raw)
	}
	portVal, convErr := strconv.Atoi(portPart)
	if convErr != nil || portVal < 1 || portVal > 65535 {
		return "", 0, "", fmt.Errorf("SSH host 端口非法: %q", raw)
	}
	return hostPart, portVal, fmt.Sprintf("%s:%d", hostPart, portVal), nil
}

func validateSSHHost(raw string) (string, error) {
	_, _, canonical, err := parseSSHHostSpec(raw)
	return canonical, err
}

// NormalizeSSHHosts 校验并去重 Hosts；非法项返回 error（供 Remote 编辑提交使用）。
func NormalizeSSHHosts(rawHosts []string) ([]string, error) {
	out := make([]string, 0, len(rawHosts))
	seen := make(map[string]struct{})
	for _, raw := range rawHosts {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		canonical, err := validateSSHHost(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out, nil
}

// parseSSHHostsNotes 解析 SSH Key Item 的 Notes 字段（可选；有内容时一行一个 host）。
func parseSSHHostsNotes(notes string) []string {
	var hosts []string
	seen := make(map[string]struct{})
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		hosts = append(hosts, line)
	}
	return hosts
}

// formatSSHHostsNotes 把 hosts 写成 Notes 正文（一行一个；空则空串）。
func formatSSHHostsNotes(hosts []string) string {
	lines := make([]string, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" || strings.HasPrefix(h, "#") {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		lines = append(lines, h)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// PrepareSSHKeyLandings 校验一批 SSH Key 并解析落地路径；不写盘。
// 批次内文件名冲突或 host 冲突会报错。私钥内容不进入错误信息。
func PrepareSSHKeyLandings(decBundleName string, keys []SSHKeyItem) ([]SSHKeyLanding, error) {
	sshDir, err := SSHDir()
	if err != nil {
		return nil, err
	}
	out := make([]SSHKeyLanding, 0, len(keys))
	seenNames := make(map[string]struct{}, len(keys))
	seenFiles := make(map[string]string, len(keys)) // filename -> key name
	seenHosts := make(map[string]string, len(keys))  // host -> key name

	for _, key := range keys {
		fileBase, err := SSHKeyFileName(decBundleName, key.Name)
		if err != nil {
			return nil, err
		}
		name, err := validateSSHSafeName("SSH Key", key.Name)
		if err != nil {
			return nil, err
		}
		if _, dup := seenNames[name]; dup {
			return nil, fmt.Errorf("SSH Key 名称重复: %q", name)
		}
		seenNames[name] = struct{}{}
		if prev, ok := seenFiles[fileBase]; ok {
			return nil, fmt.Errorf("SSH Key 文件名冲突: %q 与 %q 均映射到 %s", prev, name, fileBase)
		}
		seenFiles[fileBase] = name

		if strings.TrimSpace(key.PrivateKey) == "" {
			return nil, fmt.Errorf("SSH Key %q 缺少私钥", name)
		}
		if err := validateSSHKeyMaterial(key.PrivateKey); err != nil {
			return nil, fmt.Errorf("SSH Key %q 私钥格式无效", name)
		}

		// Hosts 可为空：仅落地密钥文件，不写 SSH config Host 条目。
		// 元素为规范串 host 或 host:port；冲突按 Host 模式（不含 port）检测。
		hosts := make([]string, 0, len(key.Hosts))
		for _, raw := range key.Hosts {
			hostPattern, _, canonical, hostErr := parseSSHHostSpec(raw)
			if hostErr != nil {
				return nil, fmt.Errorf("SSH Key %q: %w", name, hostErr)
			}
			if prev, ok := seenHosts[hostPattern]; ok {
				return nil, fmt.Errorf("SSH host %q 冲突：同时由 Key %q 与 %q 声明", hostPattern, prev, name)
			}
			seenHosts[hostPattern] = name
			hosts = append(hosts, canonical)
		}

		privPath := filepath.Join(sshDir, fileBase)
		pubPath := privPath + ".pub"
		out = append(out, SSHKeyLanding{
			DecBundleName: decBundleName,
			Name:          name,
			Hosts:         hosts,
			PrivateKey:    key.PrivateKey,
			PublicKey:     strings.TrimSpace(key.PublicKey),
			PrivatePath:   privPath,
			PublicPath:    pubPath,
			IdentityFile:  privPath,
		})
	}
	return out, nil
}

func validateSSHKeyMaterial(privateKey string) error {
	trimmed := strings.TrimSpace(privateKey)
	if trimmed == "" {
		return fmt.Errorf("empty")
	}
	// 不要求完整 PEM 解析：Bitwarden 可能存 OpenSSH 或 PKCS#8。
	// 仅拒绝明显非密钥的控制字符（除换行/制表）。
	for _, r := range trimmed {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if !unicode.IsPrint(r) {
			return fmt.Errorf("invalid")
		}
	}
	return nil
}

// ResolveBundleSSHKeys 取回单个 secrets bundle 的 SSH Keys（不写盘）。
func ResolveBundleSSHKeys(ctx context.Context, client Client, req PullBundleRequest) ([]SSHKeyItem, error) {
	_, keys, err := ResolveBundle(ctx, client, req)
	return keys, err
}

// WriteSSHKeyLandings 将已校验的 SSH Key 写入 ~/.ssh，并更新 Dec managed SSH config。
// Host 条目写入 ~/.ssh/config.d/dec.conf，主 ~/.ssh/config 顶部 Ensure Include（优先于用户 Host）。
// 仅对声明了 Hosts 的 key 写入 Host 条目；空 Hosts 只落密钥文件，并按 IdentityFile 清掉其旧条目。
// 按 IdentityFile 更新本批 keys，保留其他项目先前管理的条目。不清理远端已移除的旧 key。
func WriteSSHKeyLandings(landings []SSHKeyLanding) error {
	if len(landings) == 0 {
		return nil
	}
	sshDir, err := SSHDir()
	if err != nil {
		return err
	}
	if err := ensureSSHDir(sshDir); err != nil {
		return err
	}

	identityFiles := make([]string, 0, len(landings))
	for _, landing := range landings {
		if err := writeSecureFile(landing.PrivatePath, []byte(ensureTrailingNewline(landing.PrivateKey)), 0600); err != nil {
			return fmt.Errorf("写入 SSH 私钥文件失败: %w", err)
		}
		if landing.PublicKey != "" {
			if err := writeSecureFile(landing.PublicPath, []byte(ensureTrailingNewline(landing.PublicKey)), 0644); err != nil {
				return fmt.Errorf("写入 SSH 公钥文件失败: %w", err)
			}
		}
		identityFiles = append(identityFiles, landing.IdentityFile)
	}

	entries := make([]sshManagedEntry, 0)
	for _, landing := range landings {
		for _, raw := range landing.Hosts {
			host, port, _, hostErr := parseSSHHostSpec(raw)
			if hostErr != nil {
				return fmt.Errorf("SSH Key %q: %w", landing.Name, hostErr)
			}
			entries = append(entries, sshManagedEntry{
				Host:         host,
				Port:         port,
				IdentityFile: landing.IdentityFile,
			})
		}
	}
	return upsertManagedSSHConfig(filepath.Join(sshDir, "config"), identityFiles, entries)
}

// RemoveSSHKeyLanding 删除本地私钥/公钥，并从 managed config 移除对应 IdentityFile 条目。
func RemoveSSHKeyLanding(decBundleName, keyName string) error {
	fileBase, err := SSHKeyFileName(decBundleName, keyName)
	if err != nil {
		return err
	}
	sshDir, err := SSHDir()
	if err != nil {
		return err
	}
	privPath := filepath.Join(sshDir, fileBase)
	pubPath := privPath + ".pub"
	for _, path := range []string{privPath, pubPath} {
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("删除 %s 失败: %w", filepath.Base(path), rmErr)
		}
	}
	return removeManagedSSHConfigEntries(filepath.Join(sshDir, "config"), []string{privPath})
}

func ensureTrailingNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func ensureSSHDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建 %s 失败: %w", dir, err)
	}
	return restrictDirPermissions(dir)
}

// LocalSSHKeyExists 检查本地私钥或公钥是否存在。
func LocalSSHKeyExists(decBundleName, keyName string) (bool, error) {
	fileBase, err := SSHKeyFileName(decBundleName, keyName)
	if err != nil {
		return false, err
	}
	sshDir, err := SSHDir()
	if err != nil {
		return false, err
	}
	priv := filepath.Join(sshDir, fileBase)
	pub := priv + ".pub"
	if _, err := os.Stat(priv); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if _, err := os.Stat(pub); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	return false, nil
}
