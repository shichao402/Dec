package legacy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status 是单项执行结果。
type Status string

const (
	StatusDeleted  Status = "deleted"
	StatusStripped Status = "stripped"
	// StatusSkipped 表示归档后对象被改过，按旧归档删掉会丢掉新内容，所以不动。
	StatusSkipped Status = "skipped"
	StatusFailed  Status = "failed"
)

// ItemResult 是一项命中的执行结果（需求 4.5）。
type ItemResult struct {
	Item   Item   `json:"item"`
	Status Status `json:"status"`
	Reason string `json:"reason,omitempty"`
	// Removed 是 strip 实际删掉的条目。
	Removed []string `json:"removed,omitempty"`
	// Stopped 是删除前结束的进程。
	Stopped []string `json:"stopped,omitempty"`
}

// Result 是一次执行的结果。
type Result struct {
	// Empty 为真时没有可清理的内容，也没有产生归档（需求 3.3）。
	Empty   bool         `json:"empty"`
	Archive *Archive     `json:"archive,omitempty"`
	Items   []ItemResult `json:"items"`
	// Listed 是只列出、不处理的凭据（需求 2.6、5.4）。
	Listed []Item `json:"listed,omitempty"`
	// Repos 是项目仓库的自动提交结果（需求 8）。
	Repos []RepoResult `json:"repos,omitempty"`
}

// Failed 报告是否有失败项。
func (r *Result) Failed() bool {
	for _, it := range r.Items {
		if it.Status == StatusFailed {
			return true
		}
	}
	return false
}

// ExecOptions 是执行的可注入依赖。
type ExecOptions struct {
	Now   func() time.Time
	Procs Processes
	Git   GitOptions
}

// Execute 按扫描结果执行清理：先归档并校验，再逐项删除（需求 3.2、4.2、4.5、5.1–5.3）。
//
// 归档失败时返回错误，什么都不动。归档成功后单项失败不回滚、不中止，逐项报告。
// 调用方必须传入刚扫描出的结果；这里不会重新扫描，但每项删除前都会按归档核对现状。
func Execute(stateDir string, report *Report, opts ExecOptions) (*Result, error) {
	if report == nil {
		return nil, errors.New("缺少扫描结果")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Procs == nil {
		opts.Procs = SystemProcesses()
	}
	res := &Result{Items: []ItemResult{}}
	for _, it := range report.Items {
		if it.Action == ActionList {
			res.Listed = append(res.Listed, it)
		}
	}
	if report.Empty() {
		res.Empty = true
		return res, nil
	}

	a, err := WriteArchive(stateDir, report.Items, opts.Now())
	if err != nil {
		return nil, fmt.Errorf("归档失败，未删除任何内容：%w", err)
	}
	res.Archive = a

	byItem := map[int][]ArchiveEntry{}
	for _, e := range a.Manifest.Entries {
		byItem[e.Item] = append(byItem[e.Item], e)
	}
	for i, it := range a.Manifest.Items {
		var r ItemResult
		if it.Action == ActionStrip {
			r = stripItem(it, byItem[i])
		} else {
			r = deleteItem(it, byItem[i], opts.Procs)
		}
		res.Items = append(res.Items, r)
	}
	res.Repos = commitProjects(res, opts.Git)
	return res, nil
}

// processRules 是删除前要先结束其中进程的规则（rules.md H01、H04、H06）。
var processRules = map[string]bool{RuleDecHome: true, RuleConsoleApp: true, RuleOldBinaries: true}

func deleteItem(it Item, entries []ArchiveEntry, procs Processes) ItemResult {
	r := ItemResult{Item: it}
	if _, err := os.Lstat(it.Path); errors.Is(err, fs.ErrNotExist) {
		r.Status = StatusDeleted
		r.Reason = "已不存在"
		return r
	}
	if reason := changedSince(it.Path, entries); reason != "" {
		r.Status, r.Reason = StatusSkipped, reason
		return r
	}
	if processRules[it.Rule] {
		stopped, err := procs.StopWithin(it.Path)
		r.Stopped = stopped
		if err != nil {
			r.Status, r.Reason = StatusFailed, "结束进程失败："+err.Error()
			return r
		}
	}
	if err := os.RemoveAll(it.Path); err != nil {
		r.Status, r.Reason = StatusFailed, err.Error()
		return r
	}
	r.Status = StatusDeleted
	return r
}

// changedSince 核对对象与归档时是否一致。归档后新增、类型变化、内容变化都算变了；
// 归档后已被删掉的子项不算，删掉它们本来就是目的。
func changedSince(root string, entries []ArchiveEntry) string {
	want := make(map[string]ArchiveEntry, len(entries))
	for _, e := range entries {
		want[e.Path] = e
	}
	var reason string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		e, ok := want[p]
		if !ok {
			reason = "归档后新增了 " + p
			return fs.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if typeOf(info) != e.Type {
			reason = "归档后类型变了：" + p
			return fs.SkipAll
		}
		switch e.Type {
		case EntryFile:
			if info.Size() != e.Size || !info.ModTime().UTC().Equal(e.ModTime) {
				reason = "归档后被修改：" + p
				return fs.SkipAll
			}
		case EntrySymlink:
			if t, err := os.Readlink(p); err != nil || t != e.LinkTarget {
				reason = "归档后链接目标变了：" + p
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "核对现状失败：" + err.Error()
	}
	return reason
}

func typeOf(info fs.FileInfo) EntryType {
	switch m := info.Mode(); {
	case m.IsDir():
		return EntryDir
	case m&fs.ModeSymlink != 0:
		return EntrySymlink
	case m.IsRegular():
		return EntryFile
	default:
		return EntryOther
	}
}

// stripItem 只删共享文件里的旧版条目（需求 5.1、5.2）。
//
// 原文件是软链接时改的是它指向的文件，链接本身保留：~/.zshrc 指向 dotfiles 仓库时，
// 把链接换成普通文件会让用户的 dotfiles 管理悄悄断开。
// 要删的条目按当前内容重新认定，不信任扫描时的行号；但当前内容必须与归档一致，
// 否则归档里的那份不是删除前的原文，还原会丢掉用户在中间做的修改。
func stripItem(it Item, entries []ArchiveEntry) ItemResult {
	r := ItemResult{Item: it}
	if len(entries) != 1 || entries[0].Type != EntryFragment {
		r.Status, r.Reason = StatusFailed, "归档中缺少这份文件的原文"
		return r
	}
	e := entries[0]
	target, err := filepath.EvalSymlinks(it.Path)
	if err != nil {
		r.Status, r.Reason = StatusFailed, err.Error()
		return r
	}
	data, err := os.ReadFile(target)
	if err != nil {
		r.Status, r.Reason = StatusFailed, err.Error()
		return r
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != e.SHA256 {
		r.Status, r.Reason = StatusSkipped, "归档后被修改："+it.Path
		return r
	}

	var out []byte
	var removed []string
	switch {
	case it.Rule == RuleShellRC:
		out, removed = stripShellRC(data)
	case strings.HasSuffix(it.Path, ".toml"):
		out, removed = stripTOMLServers(data, it.Entries)
	default:
		out, removed, err = stripJSONServers(data, it.Entries)
	}
	if err != nil {
		r.Status, r.Reason = StatusFailed, err.Error()
		return r
	}
	if len(removed) == 0 {
		r.Status, r.Reason = StatusStripped, "没有需要删除的条目"
		return r
	}
	if err := replaceFile(target, out); err != nil {
		r.Status, r.Reason = StatusFailed, err.Error()
		return r
	}
	r.Status, r.Removed = StatusStripped, removed
	return r
}

// splitKeepEOL 按行切分并保留行尾，便于原样拼回（含 CRLF）。
func splitKeepEOL(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.SplitAfter(string(data), "\n")
}

func stripShellRC(data []byte) ([]byte, []string) {
	var buf strings.Builder
	var removed []string
	for _, line := range splitKeepEOL(data) {
		if line == "" {
			continue
		}
		if isShellRCLine(strings.TrimRight(line, "\r\n")) {
			removed = append(removed, strings.TrimSpace(line))
			continue
		}
		buf.WriteString(line)
	}
	return []byte(buf.String()), removed
}

// stripTOMLServers 删掉 [mcp_servers.<名字>] 及其子表（如 .env），其余行原样保留。
// 子表是条目的一部分，只删主表会留下一个孤立的 env 表，宿主会把它当成一条坏条目。
func stripTOMLServers(data []byte, names []string) ([]byte, []string) {
	drop := map[string]bool{}
	for _, n := range names {
		drop[n] = true
	}
	var buf strings.Builder
	var removed []string
	seen := map[string]bool{}
	skipping := false
	for _, line := range splitKeepEOL(data) {
		if line == "" {
			continue
		}
		if segs, ok := tomlHeader(strings.TrimSpace(line)); ok {
			skipping = len(segs) >= 2 && segs[0] == "mcp_servers" && drop[segs[1]]
			if skipping && !seen[segs[1]] {
				seen[segs[1]] = true
				removed = append(removed, segs[1])
			}
		}
		if !skipping {
			buf.WriteString(line)
		}
	}
	return []byte(buf.String()), removed
}

// stripJSONServers 从 mcpServers / mcp_servers 里删掉指定键。
//
// 在原始字节上切掉对应成员，其余字节一个不改：用户的键序、缩进、其他配置都不受影响。
func stripJSONServers(data []byte, names []string) ([]byte, []string, error) {
	drop := map[string]bool{}
	for _, n := range names {
		drop[n] = true
	}
	root, err := objectMembers(data)
	if err != nil {
		return nil, nil, fmt.Errorf("不是合法的 JSON 对象，拒绝改写：%w", err)
	}
	var removed []string
	out := data
	// 从后往前改，前面成员的偏移不受影响。
	for i := len(root) - 1; i >= 0; i-- {
		m := root[i]
		if m.key != "mcpServers" && m.key != "mcp_servers" {
			continue
		}
		val := data[m.valStart:m.end]
		inner, err := objectMembers(val)
		if err != nil {
			continue
		}
		newVal, got := spliceMembers(val, inner, drop)
		if len(got) == 0 {
			continue
		}
		removed = append(got, removed...)
		out = append(append(append([]byte{}, out[:m.valStart]...), newVal...), out[m.end:]...)
	}
	var probe any
	if err := json.Unmarshal(out, &probe); err != nil {
		return nil, nil, fmt.Errorf("改写结果不是合法 JSON：%w", err)
	}
	return out, removed, nil
}

type member struct {
	key string
	// start 是上一个成员结束（或 '{' 之后）的位置，含分隔用的逗号与空白；
	// valStart、end 是值本身的范围。
	start, valStart, end int
}

// objectMembers 返回一个 JSON 对象的成员及其字节范围。
func objectMembers(data []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("顶层不是对象")
	}
	var out []member
	// More() 会吞掉下一个成员前的空白，所以成员起点取上一个成员的终点，
	// 而不是读键之前的偏移；否则删第一个键时会留下一行空白。
	prev := int(dec.InputOffset())
	for dec.More() {
		start := prev
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("对象键不是字符串")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		end := int(dec.InputOffset())
		out = append(out, member{key: key, start: start, valStart: end - len(raw), end: end})
		prev = end
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("对象之后还有多余内容")
	}
	return out, nil
}

// spliceMembers 去掉 drop 中的成员，保留其余成员原有的空白与格式。
func spliceMembers(obj []byte, ms []member, drop map[string]bool) ([]byte, []string) {
	var removed []string
	var kept []member
	for _, m := range ms {
		if drop[m.key] {
			removed = append(removed, m.key)
		} else {
			kept = append(kept, m)
		}
	}
	if len(removed) == 0 {
		return obj, nil
	}
	var buf bytes.Buffer
	buf.Write(obj[:ms[0].start])
	for i, m := range kept {
		seg := obj[m.start:m.end]
		// 去掉成员前的逗号（第一个成员原本就没有），再按新位置决定要不要补。
		if j := bytes.IndexFunc(seg, func(r rune) bool { return r != ' ' && r != '\t' && r != '\n' && r != '\r' }); j >= 0 && seg[j] == ',' {
			seg = append(append([]byte{}, seg[:j]...), seg[j+1:]...)
		}
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(seg)
	}
	if len(kept) == 0 {
		// 全删光时直接收成空对象，避免留下 {\n    \n  } 这类空洞。
		return []byte("{}"), removed
	}
	buf.Write(obj[ms[len(ms)-1].end:])
	return buf.Bytes(), removed
}

// replaceFile 原子替换文件内容，保留原权限。
func replaceFile(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dec-legacy-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
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
	if err := os.Chmod(name, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(name, path)
}
