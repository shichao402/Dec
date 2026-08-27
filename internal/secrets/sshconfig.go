package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// OpenSSH 自 7.3 起支持 Include。Dec 将 managed Host 写入独立文件，
// 并在 ~/.ssh/config 最顶部 Ensure Include，使注入配置优先于用户后续 Host（先匹配先生效）。
const (
	sshManagedIncludeRel  = "config.d/dec.conf"
	sshManagedIncludeLine = "Include " + sshManagedIncludeRel
)

type sshManagedEntry struct {
	Host         string
	Port         int // 0 = 不写 Port（OpenSSH 默认 22）
	IdentityFile string
}

func sshManagedConfigPath(sshDir string) string {
	return filepath.Join(sshDir, "config.d", "dec.conf")
}

// upsertManagedSSHConfig 更新 Dec managed SSH Host 配置（分文件 + 主 config 顶部 Include）。
// removeIdentityFiles：先移除这些 IdentityFile 的旧条目，再写入 entries。
// 会把旧版内嵌于 ~/.ssh/config 的 BEGIN/END DEC MANAGED 块迁移到 config.d/dec.conf。
func upsertManagedSSHConfig(configPath string, removeIdentityFiles []string, entries []sshManagedEntry) error {
	sshDir := filepath.Dir(configPath)
	managedPath := sshManagedConfigPath(sshDir)

	existing, userBody, err := loadSSHManagedState(configPath, managedPath)
	if err != nil {
		return err
	}
	removeSet := make(map[string]struct{}, len(removeIdentityFiles))
	for _, id := range removeIdentityFiles {
		removeSet[normalizeIdentityPath(id)] = struct{}{}
	}
	kept := make([]sshManagedEntry, 0, len(existing)+len(entries))
	for _, e := range existing {
		if _, drop := removeSet[normalizeIdentityPath(e.IdentityFile)]; drop {
			continue
		}
		kept = append(kept, e)
	}
	kept = append(kept, entries...)
	return writeSSHManagedState(configPath, managedPath, userBody, kept)
}

func removeManagedSSHConfigEntries(configPath string, identityFiles []string) error {
	sshDir := filepath.Dir(configPath)
	managedPath := sshManagedConfigPath(sshDir)

	existing, userBody, err := loadSSHManagedState(configPath, managedPath)
	if err != nil {
		return err
	}
	if len(existing) == 0 && !strings.Contains(userBody, sshManagedIncludeLine) &&
		!fileExists(managedPath) {
		return nil
	}
	removeSet := make(map[string]struct{}, len(identityFiles))
	for _, id := range identityFiles {
		removeSet[normalizeIdentityPath(id)] = struct{}{}
	}
	kept := make([]sshManagedEntry, 0, len(existing))
	for _, e := range existing {
		if _, drop := removeSet[normalizeIdentityPath(e.IdentityFile)]; drop {
			continue
		}
		kept = append(kept, e)
	}
	return writeSSHManagedState(configPath, managedPath, userBody, kept)
}

func loadSSHManagedState(configPath, managedPath string) (entries []sshManagedEntry, userBody string, err error) {
	raw, err := readFileOrEmpty(configPath)
	if err != nil {
		return nil, "", err
	}
	prefix, legacyManaged, suffix, err := splitManagedBlock(raw)
	if err != nil {
		return nil, "", err
	}
	userBody = stripDecIncludeLines(prefix + suffix)

	fileManaged, err := readFileOrEmpty(managedPath)
	if err != nil {
		return nil, "", err
	}
	// 分文件已有内容则以之为准；否则吃旧内嵌块（一次性迁移）。
	managedRaw := fileManaged
	if strings.TrimSpace(managedRaw) == "" {
		managedRaw = legacyManaged
	}
	return parseManagedEntries(managedRaw), userBody, nil
}

func writeSSHManagedState(configPath, managedPath, userBody string, entries []sshManagedEntry) error {
	if err := os.MkdirAll(filepath.Dir(managedPath), 0700); err != nil {
		return fmt.Errorf("创建 SSH config.d 失败: %w", err)
	}
	if err := restrictDirPermissions(filepath.Dir(managedPath)); err != nil {
		return err
	}

	newManaged := renderManagedBlock(entries)
	if strings.TrimSpace(newManaged) == "" {
		if err := os.Remove(managedPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除 Dec managed SSH config 失败: %w", err)
		}
		out := strings.TrimLeft(userBody, "\r\n")
		if strings.TrimSpace(out) == "" {
			out = ""
		}
		return writeSecureFile(configPath, []byte(out), 0600)
	}

	if err := writeSecureFile(managedPath, []byte(newManaged), 0600); err != nil {
		return fmt.Errorf("写入 Dec managed SSH config 失败: %w", err)
	}
	out := ensureDecIncludeAtTop(userBody)
	return writeSecureFile(configPath, []byte(out), 0600)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readFileOrEmpty(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	return string(data), nil
}

func splitManagedBlock(raw string) (prefix, managed, suffix string, err error) {
	beginIdx := strings.Index(raw, sshManagedBegin)
	if beginIdx < 0 {
		return raw, "", "", nil
	}
	afterBegin := beginIdx + len(sshManagedBegin)
	// 跳过 BEGIN 行尾换行
	if afterBegin < len(raw) && raw[afterBegin] == '\r' {
		afterBegin++
	}
	if afterBegin < len(raw) && raw[afterBegin] == '\n' {
		afterBegin++
	}
	endIdx := strings.Index(raw[afterBegin:], sshManagedEnd)
	if endIdx < 0 {
		return "", "", "", fmt.Errorf("SSH config 中有 %s 但缺少 %s", sshManagedBegin, sshManagedEnd)
	}
	endIdx += afterBegin
	prefix = raw[:beginIdx]
	managed = raw[afterBegin:endIdx]
	afterEnd := endIdx + len(sshManagedEnd)
	if afterEnd < len(raw) && raw[afterEnd] == '\r' {
		afterEnd++
	}
	if afterEnd < len(raw) && raw[afterEnd] == '\n' {
		afterEnd++
	}
	suffix = raw[afterEnd:]
	return prefix, managed, suffix, nil
}

func isDecIncludeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "Include") {
		return false
	}
	target := strings.Trim(fields[1], `"'`)
	target = strings.ReplaceAll(target, "\\", "/")
	target = strings.TrimPrefix(target, "~/")
	target = strings.TrimPrefix(target, strings.TrimSuffix(strings.ReplaceAll(os.Getenv("USERPROFILE"), "\\", "/"), "/")+"/")
	if home, err := os.UserHomeDir(); err == nil {
		homeSlash := strings.ReplaceAll(home, "\\", "/")
		target = strings.TrimPrefix(target, homeSlash+"/")
		target = strings.TrimPrefix(target, homeSlash+"/.ssh/")
	}
	target = strings.TrimPrefix(target, ".ssh/")
	return target == sshManagedIncludeRel || strings.HasSuffix(target, "/"+sshManagedIncludeRel)
}

func stripDecIncludeLines(raw string) string {
	var b strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		if isDecIncludeLine(line) {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func ensureDecIncludeAtTop(userBody string) string {
	body := strings.TrimLeft(stripDecIncludeLines(userBody), "\r\n")
	if body == "" {
		return sshManagedIncludeLine + "\n"
	}
	return sshManagedIncludeLine + "\n\n" + body
}

func parseManagedEntries(managed string) []sshManagedEntry {
	var entries []sshManagedEntry
	var currentHosts []string
	var identity string
	var port int

	flush := func() {
		if identity == "" || len(currentHosts) == 0 {
			currentHosts = nil
			identity = ""
			port = 0
			return
		}
		for _, host := range currentHosts {
			entries = append(entries, sshManagedEntry{Host: host, Port: port, IdentityFile: identity})
		}
		currentHosts = nil
		identity = ""
		port = 0
	}

	for _, line := range strings.Split(managed, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		keyword := strings.ToLower(fields[0])
		switch keyword {
		case "host":
			flush()
			if len(fields) > 1 {
				currentHosts = append([]string(nil), fields[1:]...)
			}
		case "identityfile":
			if len(fields) > 1 {
				identity = strings.Trim(strings.Join(fields[1:], " "), `"'`)
			}
		case "port":
			if len(fields) > 1 {
				if p, err := strconv.Atoi(fields[1]); err == nil && p >= 1 && p <= 65535 {
					port = p
				}
			}
		}
	}
	flush()
	return entries
}

func renderManagedBlock(entries []sshManagedEntry) string {
	if len(entries) == 0 {
		return ""
	}
	// 按 IdentityFile + Port 分组：同 key 且同端口的多个 Host 合并到一个 Host 行。
	type group struct {
		hosts []string
		id    string
		port  int
	}
	order := make([]string, 0)
	grouped := make(map[string]*group)
	for _, e := range entries {
		key := normalizeIdentityPath(e.IdentityFile) + "\x00" + strconv.Itoa(e.Port)
		g, ok := grouped[key]
		if !ok {
			g = &group{id: e.IdentityFile, port: e.Port}
			grouped[key] = g
			order = append(order, key)
		}
		g.hosts = append(g.hosts, e.Host)
	}

	var b strings.Builder
	b.WriteString(sshManagedBegin)
	b.WriteByte('\n')
	for _, key := range order {
		g := grouped[key]
		b.WriteString("Host ")
		b.WriteString(strings.Join(g.hosts, " "))
		b.WriteByte('\n')
		if g.port != 0 {
			b.WriteString("  Port ")
			b.WriteString(strconv.Itoa(g.port))
			b.WriteByte('\n')
		}
		b.WriteString("  IdentityFile ")
		b.WriteString(g.id)
		b.WriteByte('\n')
	}
	b.WriteString(sshManagedEnd)
	b.WriteByte('\n')
	return b.String()
}

func normalizeIdentityPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	// 展开 ~/ 便于与绝对路径比较。
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	cleaned := filepath.Clean(p)
	return strings.ToLower(cleaned)
}
