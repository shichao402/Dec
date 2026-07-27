package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type sshManagedEntry struct {
	Host         string
	IdentityFile string
}

// upsertManagedSSHConfig 更新 ~/.ssh/config 的 DEC MANAGED 区块。
// removeIdentityFiles：先移除这些 IdentityFile 的旧条目，再写入 entries。
// 其他 IdentityFile（其他项目）的条目全部保留；区块外用户配置原样保留。
func upsertManagedSSHConfig(configPath string, removeIdentityFiles []string, entries []sshManagedEntry) error {
	raw, err := readFileOrEmpty(configPath)
	if err != nil {
		return err
	}
	prefix, managed, suffix, err := splitManagedBlock(raw)
	if err != nil {
		return err
	}
	existing := parseManagedEntries(managed)
	removeSet := make(map[string]struct{}, len(removeIdentityFiles))
	for _, id := range removeIdentityFiles {
		removeSet[normalizeIdentityPath(id)] = struct{}{}
	}
	kept := make([]sshManagedEntry, 0, len(existing))
	for _, e := range existing {
		if _, drop := removeSet[normalizeIdentityPath(e.IdentityFile)]; drop {
			continue
		}
		kept = append(kept, e)
	}
	kept = append(kept, entries...)
	newManaged := renderManagedBlock(kept)
	out := joinConfigParts(prefix, newManaged, suffix)
	return writeSecureFile(configPath, []byte(out), 0600)
}

func removeManagedSSHConfigEntries(configPath string, identityFiles []string) error {
	raw, err := readFileOrEmpty(configPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	prefix, managed, suffix, err := splitManagedBlock(raw)
	if err != nil {
		return err
	}
	if managed == "" && !strings.Contains(raw, sshManagedBegin) {
		return nil
	}
	existing := parseManagedEntries(managed)
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
	newManaged := ""
	if len(kept) > 0 {
		newManaged = renderManagedBlock(kept)
	}
	out := joinConfigParts(prefix, newManaged, suffix)
	if strings.TrimSpace(out) == "" {
		// 若文件只剩空白，保留空文件以免误删用户可能依赖的路径。
		out = ""
	}
	return writeSecureFile(configPath, []byte(out), 0600)
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

func parseManagedEntries(managed string) []sshManagedEntry {
	var entries []sshManagedEntry
	var currentHosts []string
	var identity string

	flush := func() {
		if identity == "" || len(currentHosts) == 0 {
			currentHosts = nil
			identity = ""
			return
		}
		for _, host := range currentHosts {
			entries = append(entries, sshManagedEntry{Host: host, IdentityFile: identity})
		}
		currentHosts = nil
		identity = ""
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
				identity = strings.Join(fields[1:], " ")
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
	// 按 IdentityFile 分组，同一 key 的多个 Host 合并到一个 Host 行。
	type group struct {
		hosts []string
		id    string
	}
	order := make([]string, 0)
	grouped := make(map[string]*group)
	for _, e := range entries {
		key := normalizeIdentityPath(e.IdentityFile)
		g, ok := grouped[key]
		if !ok {
			g = &group{id: e.IdentityFile}
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
		b.WriteString("  IdentityFile ")
		b.WriteString(g.id)
		b.WriteByte('\n')
	}
	b.WriteString(sshManagedEnd)
	b.WriteByte('\n')
	return b.String()
}

func joinConfigParts(prefix, managed, suffix string) string {
	var b strings.Builder
	b.WriteString(prefix)
	if managed != "" {
		// 若 prefix 非空且不以换行结尾，补一个换行再写 managed。
		if len(prefix) > 0 && !strings.HasSuffix(prefix, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(managed)
	}
	b.WriteString(suffix)
	return b.String()
}

func normalizeIdentityPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	// 展开 ~/ 便于与绝对路径比较。
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	cleaned := filepath.Clean(p)
	return strings.ToLower(cleaned)
}
