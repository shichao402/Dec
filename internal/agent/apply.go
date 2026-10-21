package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/proto"
)

// 落盘（需求 2.7、6.3、6.4）。
//
// 两条硬规则：
//
//  1. 含密钥的文件以 0600 写入，其余 0644。
//  2. 共享配置文件（MCP）只改带 dec- 前缀的条目，其它条目原样保留；
//     这类文件的哈希也只按 Dec 管理的条目算，否则用户加了个自己的 MCP
//     就会被当成“本地改动”反复上报。
const (
	// entryPrefix 是 Dec 在共享配置文件里托管条目的统一前缀。
	entryPrefix = "dec-"
	// managedNotice 是写进落点的说明（需求 6.4）。
	managedNotice = "由 Dec 管理，本地修改会被覆盖；要改请改云端。"
	// decEntryComment 是 JSON 里无法加注释时的替代：用保留键承载说明。
	noticeKey = "_dec_notice"
)

// ErrNotAllowed 表示落点不在白名单内（需求 2.3）。
var ErrNotAllowed = errors.New("target not allowed")

// WriteResult 是单个文件落盘后的结果。
type WriteResult struct {
	// ID 是条目标识。
	ID string
	// Paths 是实际写入的绝对路径。
	Paths []string
	// Hash 是落盘后重新计算的内容哈希。
	Hash string
	// Secret 标记内容含密钥。
	Secret bool
}

// Writer 负责把下发内容落到本机。
type Writer struct {
	resolver *Resolver
	// home 是用户目录，用于给整文件落点加说明。
	home string
}

// NewWriter 构造落盘器。
func NewWriter(r *Resolver, home string) *Writer {
	return &Writer{resolver: r, home: home}
}

// Apply 写入一个下发文件到它的全部落点（需求 2.2 的第二个原语）。
//
// 一个下发可能对应多个落点（同一份 rule 落到多个 IDE），任一落点失败都返回错误，
// 但已写成功的落点保留：重复下发是幂等的，下次会补齐失败的。
func (w *Writer) Apply(f proto.File) (*WriteResult, error) {
	if len(f.Targets) == 0 {
		return nil, fmt.Errorf("%w: 条目 %s 没有落点", ErrNotAllowed, f.ID)
	}
	res := &WriteResult{ID: f.ID, Secret: f.Secret}
	var paths []string
	var lastHash string
	for _, t := range f.Targets {
		full, err := w.resolver.Resolve(t)
		if err != nil {
			return nil, err
		}
		hash, err := w.writeOne(full, f)
		if err != nil {
			return nil, err
		}
		paths = append(paths, full)
		lastHash = hash
	}
	sort.Strings(paths)
	res.Paths = paths
	res.Hash = lastHash
	return res, nil
}

// writeOne 写一个落点，返回该落点的内容哈希。
func (w *Writer) writeOne(full string, f proto.File) (string, error) {
	if !isWithin(w.home, full) {
		return "", fmt.Errorf("%w: 落点 %q 不在用户目录内", ErrNotAllowed, full)
	}
	mode := os.FileMode(0o644)
	if f.Secret || f.Mode != 0 {
		mode = os.FileMode(f.Mode)
		if mode == 0 {
			mode = 0o600
		}
	}

	switch f.Format {
	case proto.FormatWhole:
		content := w.withNotice(full, f.Content, f.Secret)
		if err := writeFileAtomic(full, []byte(content), mode); err != nil {
			return "", err
		}
		return hashOf([]byte(content)), nil

	case proto.FormatJSONKey:
		if !strings.HasPrefix(f.Key, entryPrefix) {
			return "", fmt.Errorf("%w: 键名 %q 必须以 %s 开头", ErrNotAllowed, f.Key, entryPrefix)
		}
		merged, err := mergeJSONKey(full, f.Key, f.Content)
		if err != nil {
			return "", err
		}
		if err := writeFileAtomic(full, merged, mode); err != nil {
			return "", err
		}
		// 哈希只算 Dec 管理的条目（需求 6.3）。
		return hashOf(managedOnly(full, merged, "")), nil
	}
	return "", fmt.Errorf("%w: 未知的落盘格式 %q", ErrNotAllowed, f.Format)
}

// Remove 删除一个下发文件（需求 2.2 的第三个原语）。
//
// 整文件格式直接删文件；json_key 格式只删自己那个键，
// 删完没有托管条目就顺带删掉整个文件——但那要确认文件里确实只剩空容器，
// 否则会把用户自己的配置一起删掉。
func (w *Writer) Remove(f proto.File) (*WriteResult, error) {
	res := &WriteResult{ID: f.ID}
	var paths []string
	for _, t := range f.Targets {
		full, err := w.resolver.Resolve(t)
		if err != nil {
			// 删除时路径可能已经不在白名单内（例如项目被移出登记）。
			// 这种情况跳过而不是报错：目标本来就不该存在。
			continue
		}
		if f.Format == proto.FormatJSONKey && f.Key != "" {
			if err := removeJSONKey(full, f.Key); err != nil {
				return nil, err
			}
			paths = append(paths, full)
			continue
		}
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("删除 %q 失败: %w", full, err)
		}
		w.removeEmptyParents(full)
		paths = append(paths, full)
	}
	sort.Strings(paths)
	res.Paths = paths
	return res, nil
}

// removeEmptyParents 删除因删除文件而变空的 Dec 落点目录。
//
// 只删到“类别目录”为止（skills / rules / env），不继续往上，
// 因为再往上是 IDE 自己的目录，属于别人的东西。
func (w *Writer) removeEmptyParents(full string) {
	dir := filepath.Dir(full)
	for i := 0; i < 4; i++ {
		base := filepath.Base(dir)
		if base != DirSkills && base != DirRules && base != DirEnv {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// withNotice 给整文件落点加一条“由 Dec 管理”的说明（需求 6.4）。
//
// 选择把说明写在文件里而不是只写在目录里：用户打开的就是这个文件，
// 提示必须出现在他看到内容的地方。对含密钥的文件，说明本身不含任何敏感信息。
func (w *Writer) withNotice(full, content string, secret bool) string {
	ext := strings.ToLower(filepath.Ext(full))
	switch ext {
	case ".json":
		// JSON 不能加注释，且内容可能是被整体覆盖的配置文件，
		// 往里塞额外键会改变语义，所以只对“非共享”文件加。
		return content
	case ".md", ".mdc", ".txt", ".yaml", ".yml", "":
		if strings.Contains(content, managedNotice) {
			return content
		}
		if secret {
			// 含密钥的文件不写说明：内容是 env 或密钥模板，
			// 多一行注释可能被某些解析器当成变量。
			return content
		}
		return "<!-- " + managedNotice + " -->\n" + content
	}
	return content
}

// hashOf 计算内容哈希，与服务端同一算法（ssot.SumHex）。
func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// HashFile 读取落点并计算哈希，用于上报本地清单（需求 2.1 的第五个原语）。
//
// json_key 格式的落点只算 Dec 管理的条目：文件里其它键由用户维护，
// 把它们算进哈希会让每一次用户改动都被误判成“Dec 落点被改”。
func HashFile(full string, format proto.Format, key string) (string, error) {
	data, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if format == proto.FormatJSONKey && key != "" {
		return hashOf(managedOnly(full, data, key)), nil
	}
	return hashOf(data), nil
}

// managedOnly 从 JSON 内容里提取 Dec 管理的条目并序列化成稳定字节。
//
// key 为空表示“任意 dec- 前缀的键”，用于计算 json_key 落点的整体哈希。
// 排序后再序列化，保证同样的内容得到同样的哈希，不受原始键顺序影响。
func managedOnly(full string, data []byte, key string) []byte {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		// 不是 JSON 就按原始字节算：这样至少能发现文件被改动，
		// 也不会因为解析失败而把落点当成“不存在”。
		return data
	}
	out := map[string]json.RawMessage{}
	for k, v := range root {
		if k == noticeKey {
			continue
		}
		if key != "" {
			if k == key {
				out[k] = v
			}
			continue
		}
		if strings.HasPrefix(k, entryPrefix) {
			out[k] = v
		}
	}
	return marshalSorted(out)
}

// marshalSorted 按键名排序序列化，保证哈希稳定。
func marshalSorted(m map[string]json.RawMessage) []byte {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(m[k])
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// mergeJSONKey 把一个键合并进共享 JSON 配置文件（需求 6.3）。
//
// 只动目标键，其它键按原始字节回写：这样用户自己的 MCP 配置即使格式很怪，
// 也不会因为我们的反序列化而变形。文件不存在时新建。
func mergeJSONKey(full, key, value string) ([]byte, error) {
	existing, err := readFileOrEmpty(full)
	if err != nil {
		return nil, fmt.Errorf("读取 %q 失败: %w", full, err)
	}
	root := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			// 用户手写坏了共享配置文件：按空对象处理会静默丢掉他的配置，
			// 所以这里直接报错，让人来处理。
			return nil, fmt.Errorf("%w: %q 不是合法 JSON，拒绝改写", ErrNotAllowed, full)
		}
	}
	// 值必须是合法 JSON：云端给的是单条目对象。
	var probe any
	if err := json.Unmarshal([]byte(value), &probe); err != nil {
		return nil, fmt.Errorf("%w: 条目内容不是合法 JSON", ErrNotAllowed)
	}
	root[key] = json.RawMessage(value)
	return marshalIndentJSON(root), nil
}

// marshalIndentJSON 排序后缩进输出，保持文件可读。
func marshalIndentJSON(m map[string]json.RawMessage) []byte {
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	if len(m) == 0 {
		return []byte("{}\n")
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range keys {
		kb, _ := json.Marshal(k)
		buf.WriteString("  ")
		buf.Write(kb)
		buf.WriteString(": ")
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, m[k], "  ", "  "); err == nil {
			buf.Write(pretty.Bytes())
		} else {
			buf.Write(m[k])
		}
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")
	return buf.Bytes()
}

// removeJSONKey 从共享 JSON 配置文件里删掉一个键。
func removeJSONKey(full, key string) error {
	existing, err := readFileOrEmpty(full)
	if err != nil || len(existing) == 0 {
		return err
	}
	root := map[string]json.RawMessage{}
	if err := json.Unmarshal(existing, &root); err != nil {
		return fmt.Errorf("%w: %q 不是合法 JSON，拒绝改写", ErrNotAllowed, full)
	}
	if _, ok := root[key]; !ok {
		return nil
	}
	delete(root, key)
	// 只剩没有 Dec 托管条目时，把整个文件删掉，避免留下空壳。
	// 但必须先确认里面没有别人的键。
	for k := range root {
		if k != noticeKey {
			return writeFileAtomic(full, marshalIndentJSON(root), 0o644)
		}
	}
	return os.Remove(full)
}

// VerifyWithin 校验一个绝对路径是否在允许的落脚范围内，供删除回执使用。
func (w *Writer) VerifyWithin(p string) error {
	if !isWithin(w.home, p) {
		return fmt.Errorf("%w: 路径 %q 不在用户目录内", ErrNotAllowed, p)
	}
	return nil
}
