package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 本机 Agent 宿主的 MCP 自动接入（需求 2）。
//
// 目标只有一件事：让 Claude、Codex、Cursor、With 打开就能用上 Dec，不用用户自己
// 记端口、改配置。因此代理每次启动都把自己写成 `dec` 这一条 MCP 服务器，
// 端口变了下一轮同步就更新（需求 2.4）。
//
// 两条硬约束：
//
//  1. 只动 `dec` 这一条。配置文件是用户与其它工具共用的，改坏别人的条目
//     比配置没写上更糟（需求 2.2）。
//  2. 不替宿主创建目录。没装 Claude 的机器上凭空造一个 ~/.claude 会让人
//     以为装过（需求 2.3）。
const (
	// mcpEntryName 是所有宿主里统一使用的条目名。
	mcpEntryName = "dec"
)

// hostFormat 是配置文件的格式。
type hostFormat int

const (
	// formatJSON 是 JSON 对象，条目放在 mcpServers 下。
	formatJSON hostFormat = iota
	// formatTOML 是 TOML，条目是 [mcp_servers.dec] 表。
	formatTOML
)

// Host 是一个 Agent 宿主的用户级 MCP 配置位置。
type Host struct {
	// Name 是宿主的稳定标识，出现在 mcp-config 的输出里。
	Name string
	// Dir 是宿主的用户级配置目录。它不存在就跳过这个宿主。
	Dir string
	// Path 是真正要读写的配置文件。
	Path string
	// Format 是配置文件格式。
	Format hostFormat
}

// HostStatus 是一次同步的结果。
type HostStatus int

const (
	// HostWritten 表示已写入或已是最新。
	HostWritten HostStatus = iota
	// HostRemoved 表示已移除 `dec` 条目。
	HostRemoved HostStatus = iota
	// HostSkipped 表示宿主没安装，主动跳过。
	HostSkipped
	// HostFailed 表示写入失败。
	HostFailed
)

// HostResult 是单个宿主的同步结果。
type HostResult struct {
	Name   string
	Path   string
	Status HostStatus
	// Reason 说明跳过或失败的原因。
	Reason string
	// Entry 是当前文件里 `dec` 条目的内容，用于 mcp-config 打印。
	Entry string
}

// DefaultHosts 返回各宿主的用户级配置位置（需求 2.1；With 见 ADR 0054）。
//
// home 由调用方给出，测试可以指向临时目录。
func DefaultHosts(home string) []Host {
	return []Host{
		{
			// Claude Code 的用户级 MCP 配置在 ~/.claude.json，
			// 而 ~/.claude 是它的数据目录，用后者判断有没有装。
			Name:   "claude",
			Dir:    filepath.Join(home, ".claude"),
			Path:   filepath.Join(home, ".claude.json"),
			Format: formatJSON,
		},
		{
			Name:   "codex",
			Dir:    filepath.Join(home, ".codex"),
			Path:   filepath.Join(home, ".codex", "config.toml"),
			Format: formatTOML,
		},
		{
			Name:   "cursor",
			Dir:    filepath.Join(home, ".cursor"),
			Path:   filepath.Join(home, ".cursor", "mcp.json"),
			Format: formatJSON,
		},
		{
			// With 桌面端的用户级配置目录与 MCP 文件，与 host.go 落点表一致。
			// 漏了它，代理一换端口 With 就连不上 dec（ADR 0054）。
			Name:   "with",
			Dir:    filepath.Join(home, ".bg-agent", "config-with-app"),
			Path:   filepath.Join(home, ".bg-agent", "config-with-app", "mcp_config.json"),
			Format: formatJSON,
		},
	}
}

// MCPServerURL 返回本机 MCP 入口地址。
func MCPServerURL(addr string) string {
	return "http://" + addr + "/mcp"
}

// syncHosts 把 `dec` 条目写进每个已安装宿主的配置（需求 2.1、2.2、2.4）。
func syncHosts(hosts []Host, url string) []HostResult {
	out := make([]HostResult, 0, len(hosts))
	for _, h := range hosts {
		res := HostResult{Name: h.Name, Path: h.Path}
		if !dirExists(h.Dir) {
			res.Status = HostSkipped
			res.Reason = fmt.Sprintf("配置目录不存在：%s", h.Dir)
			out = append(out, res)
			continue
		}
		var err error
		switch h.Format {
		case formatJSON:
			err = upsertJSONEntry(h.Path, h.Name, url)
		case formatTOML:
			err = upsertTOMLEntry(h.Path, url)
		default:
			err = fmt.Errorf("未知配置格式")
		}
		if err != nil {
			res.Status = HostFailed
			res.Reason = err.Error()
			out = append(out, res)
			continue
		}
		res.Status = HostWritten
		res.Entry = readEntry(h)
		out = append(out, res)
	}
	return out
}

// removeHosts 从每个宿主的配置里移除 `dec` 条目（需求 2.5）。
func removeHosts(hosts []Host) []HostResult {
	out := make([]HostResult, 0, len(hosts))
	for _, h := range hosts {
		res := HostResult{Name: h.Name, Path: h.Path}
		if !dirExists(h.Dir) {
			res.Status = HostSkipped
			res.Reason = fmt.Sprintf("配置目录不存在：%s", h.Dir)
			out = append(out, res)
			continue
		}
		if !fileExists(h.Path) {
			res.Status = HostSkipped
			res.Reason = "配置文件不存在，无需移除"
			out = append(out, res)
			continue
		}
		var err error
		switch h.Format {
		case formatJSON:
			err = deleteJSONEntry(h.Path)
		case formatTOML:
			err = deleteTOMLEntry(h.Path)
		default:
			err = fmt.Errorf("未知配置格式")
		}
		if err != nil {
			res.Status = HostFailed
			res.Reason = err.Error()
			out = append(out, res)
			continue
		}
		res.Status = HostRemoved
		out = append(out, res)
	}
	return out
}

// inspectHosts 读出每个宿主当前的 `dec` 条目，不做任何修改。
func inspectHosts(hosts []Host) []HostResult {
	out := make([]HostResult, 0, len(hosts))
	for _, h := range hosts {
		res := HostResult{Name: h.Name, Path: h.Path}
		if !dirExists(h.Dir) {
			res.Status = HostSkipped
			res.Reason = fmt.Sprintf("配置目录不存在：%s", h.Dir)
			out = append(out, res)
			continue
		}
		if !fileExists(h.Path) {
			res.Status = HostSkipped
			res.Reason = "配置文件不存在"
			out = append(out, res)
			continue
		}
		entry, err := readEntryFor(h)
		if err != nil {
			res.Status = HostFailed
			res.Reason = err.Error()
			out = append(out, res)
			continue
		}
		if entry == "" {
			res.Status = HostSkipped
			res.Reason = "配置里没有 dec 条目"
			out = append(out, res)
			continue
		}
		res.Status = HostWritten
		res.Entry = entry
		out = append(out, res)
	}
	return out
}

// readEntry 读回刚写入的条目，用于输出确认。
func readEntry(h Host) string {
	entry, err := readEntryFor(h)
	if err != nil {
		return ""
	}
	return entry
}

// readEntryFor 按格式读出 `dec` 条目。
func readEntryFor(h Host) (string, error) {
	data, err := os.ReadFile(h.Path)
	if err != nil {
		return "", err
	}
	switch h.Format {
	case formatJSON:
		obj, err := decodeOrderedObject(data)
		if err != nil {
			return "", err
		}
		servers, ok := obj.get("mcpServers")
		if !ok {
			return "", nil
		}
		inner, err := decodeOrderedObject(servers)
		if err != nil {
			// mcpServers 不是对象时按「没有条目」处理，不改动它。
			return "", nil
		}
		raw, ok := inner.get(mcpEntryName)
		if !ok {
			return "", nil
		}
		return string(compactJSON(raw)), nil
	case formatTOML:
		return tomlSectionBody(data, mcpEntryName), nil
	}
	return "", nil
}

// upsertJSONEntry 在 JSON 配置里写入或替换 `dec` 条目。
//
// 只替换 mcpServers.dec 这一项，其它条目原样保留，连顺序都不动：
// 配置文件常常是用户手工编辑的，整体重排会让人以为改坏了。
func upsertJSONEntry(path, hostName, url string) error {
	fields := entryFor(hostName, url)

	data, err := readOrEmpty(path)
	if err != nil {
		return err
	}
	root, err := decodeOrderedObject(data)
	if err != nil {
		return fmt.Errorf("配置文件不是合法的 JSON 对象，未做修改：%w", err)
	}

	servers := newOrderedObject()
	if raw, ok := root.get("mcpServers"); ok {
		inner, err := decodeOrderedObject(raw)
		if err != nil {
			return errors.New("配置里的 mcpServers 不是 JSON 对象，未做修改")
		}
		servers = inner
	}
	// 用户在宿主界面里关掉了 dec（With 等宿主写成 disabled:true），
	// 代理只负责把地址改对，不能借更新之机把它重新打开。
	if old, ok := servers.get(mcpEntryName); ok {
		var prev map[string]json.RawMessage
		if json.Unmarshal(old, &prev) == nil {
			if v, ok := prev["disabled"]; ok {
				var b bool
				if json.Unmarshal(v, &b) == nil {
					fields["disabled"] = b
				}
			}
		}
	}
	entry, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	servers.set(mcpEntryName, entry)
	root.set("mcpServers", servers.encode())

	return atomicWrite(path, indentJSON(root.encode()))
}

// deleteJSONEntry 从 JSON 配置里移除 `dec` 条目。
func deleteJSONEntry(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	root, err := decodeOrderedObject(data)
	if err != nil {
		return fmt.Errorf("配置文件不是合法的 JSON 对象，未做修改：%w", err)
	}
	raw, ok := root.get("mcpServers")
	if !ok {
		return nil
	}
	servers, err := decodeOrderedObject(raw)
	if err != nil {
		return nil
	}
	if !servers.delete(mcpEntryName) {
		return nil
	}
	root.set("mcpServers", servers.encode())
	return atomicWrite(path, indentJSON(root.encode()))
}

// upsertTOMLEntry 在 TOML 配置里写入或替换 [mcp_servers.dec] 表。
//
// 用文本级替换而不是解析再序列化：TOML 库会把用户的注释与缩进重排一遍，
// 而那正是「只改 dec 这一条」要避免的（需求 2.2）。
func upsertTOMLEntry(path, url string) error {
	data, err := readOrEmpty(path)
	if err != nil {
		return err
	}
	section := []string{
		"[mcp_servers." + mcpEntryName + "]",
		`url = ` + tomlString(url),
	}
	out, err := replaceTOMLSection(data, mcpEntryName, section)
	if err != nil {
		return err
	}
	return atomicWrite(path, out)
}

// deleteTOMLEntry 移除 [mcp_servers.dec] 表。
func deleteTOMLEntry(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := replaceTOMLSection(data, mcpEntryName, nil)
	if err != nil {
		return err
	}
	return atomicWrite(path, out)
}

// replaceTOMLSection 用 section 替换 name 对应的表；section 为空表示删除。
//
// 表从它的表头开始，到下一个表头或文件结束为止。只做这一段的替换，
// 其它行原样保留，因此注释、空行、其它表都不受影响。
func replaceTOMLSection(data []byte, name string, section []string) ([]byte, error) {
	lines := splitLines(string(data))
	header := "[mcp_servers." + name + "]"
	// TOML 允许带引号的键名，两种写法都要认。
	quotedHeader := `[mcp_servers."` + name + `"]`

	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == header || trimmed == quotedHeader {
			start = i
			break
		}
		// 父表里用点号键写过同名项（如 dec = {...}）时不能追加新表：
		// 那会得到两个定义，TOML 解析直接报重复键。
		if trimmed == "[mcp_servers]" {
			for _, l := range lines[i+1:] {
				t := strings.TrimSpace(l)
				if strings.HasPrefix(t, "[") {
					break
				}
				if strings.HasPrefix(t, name+" ") || strings.HasPrefix(t, name+"=") || strings.HasPrefix(t, name+" =") {
					return nil, fmt.Errorf(
						"配置里已用内联写法定义了 mcp_servers.%s，请手工改成 [mcp_servers.%s] 表后重试", name, name)
				}
			}
		}
	}

	if start < 0 {
		if len(section) == 0 {
			return data, nil
		}
		out := strings.Join(lines, "\n")
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		if out != "" {
			out += "\n"
		}
		out += strings.Join(section, "\n") + "\n"
		return []byte(out), nil
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	// 表尾的空行属于下一段的分隔，不吞掉。
	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}

	merged := make([]string, 0, len(lines)-end+start+len(section))
	merged = append(merged, lines[:start]...)
	merged = append(merged, section...)
	tail := lines[end:]
	if len(section) > 0 && len(tail) > 0 {
		merged = append(merged, "")
	}
	merged = append(merged, tail...)
	return []byte(strings.Join(merged, "\n")), nil
}

// tomlSectionBody 返回某个表的正文（不含表头），用于打印当前条目。
func tomlSectionBody(data []byte, name string) string {
	lines := splitLines(string(data))
	header := "[mcp_servers." + name + "]"
	quotedHeader := `[mcp_servers."` + name + `"]`
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == header || trimmed == quotedHeader {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	var body []string
	for _, line := range lines[start+1:] {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		body = append(body, strings.TrimSpace(line))
	}
	return strings.Join(body, "\n")
}

// entryFor 返回某个宿主的 `dec` 条目内容。
//
// Claude 需要显式声明 type=http 才知道这是 HTTP 传输；Cursor 用 url 即可，
// 多写一个它不认识的字段只会在它自己的 UI 里显示成未知配置。
func entryFor(hostName, url string) map[string]any {
	entry := map[string]any{"url": url}
	switch hostName {
	case "claude":
		entry["type"] = "http"
	case "with":
		// With 用 transportType 区分传输方式（现有条目即为此写法），
		// 代理的 /mcp 是 streamable HTTP。
		entry["transportType"] = "streamable-http"
	}
	return entry
}

// tomlString 把字符串写成 TOML 基本字符串。
func tomlString(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
	return `"` + replacer.Replace(s) + `"`
}

// splitLines 按行切分，去掉文件末尾换行产生的空串。
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// readOrEmpty 读文件；文件不存在时返回空内容（首次写入的常见情况）。
func readOrEmpty(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// atomicWrite 原子写入：先写同目录临时文件再 rename（需求 3.1）。
//
// 配置被多个工具共用，写到一半崩溃会让宿主读到一个截断的 JSON 并
// 丢掉全部 MCP 配置。rename 在同一文件系统内是原子的。
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".dec-mcp-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// dirExists 报告路径是否存在且是目录。
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// fileExists 报告路径是否存在且是普通文件。
func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// orderedObject 是一个保持键顺序的 JSON 对象。
//
// 用 map 会在序列化时按字母序重排，把用户手写的顺序打乱；
// 这个类型只为「原样保留其它条目」而存在。
type orderedObject struct {
	keys   []string
	values map[string]json.RawMessage
}

// newOrderedObject 构造空对象。
func newOrderedObject() *orderedObject {
	return &orderedObject{values: map[string]json.RawMessage{}}
}

// get 取一个键的原始值。
func (o *orderedObject) get(key string) (json.RawMessage, bool) {
	v, ok := o.values[key]
	return v, ok
}

// set 写入或替换一个键，保持原位置。
func (o *orderedObject) set(key string, raw json.RawMessage) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = raw
}

// delete 移除一个键，返回是否真的移除。
func (o *orderedObject) delete(key string) bool {
	if _, ok := o.values[key]; !ok {
		return false
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
	return true
}

// encode 序列化成紧凑 JSON。
func (o *orderedObject) encode() json.RawMessage {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, _ := json.Marshal(k)
		buf.Write(key)
		buf.WriteByte(':')
		raw := o.values[k]
		if len(raw) == 0 {
			raw = json.RawMessage("null")
		}
		buf.Write(raw)
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// decodeOrderedObject 解析一个 JSON 对象并保留键顺序。
func decodeOrderedObject(data []byte) (*orderedObject, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return newOrderedObject(), nil
	}
	if trimmed[0] != '{' {
		return nil, errors.New("不是 JSON 对象")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("不是 JSON 对象")
	}

	obj := newOrderedObject()
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, errors.New("JSON 键不是字符串")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		obj.set(key, compactJSON(raw))
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return obj, nil
}

// compactJSON 去掉 JSON 里的多余空白。
func compactJSON(raw json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}

// indentJSON 把紧凑 JSON 排成两空格缩进，与 Cursor 自己写出的格式一致。
func indentJSON(raw json.RawMessage) []byte {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return raw
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}
