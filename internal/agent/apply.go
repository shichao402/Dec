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
	// decEntryComment 是 JSON 里无法加注释时的替代：用保留键承载说明。
	noticeKey = "_dec_notice"
	// mcpServersKey 是各 JSON 宿主放 MCP 服务器定义的容器键。
	//
	// 这个键不是 Dec 自己的约定，是宿主要求：cursor / claude / codebuddy /
	// with 都只读这个对象下的键。写到文件顶层的条目是合法 JSON，
	// 只是永远没人读，所以这个错误只能靠人来发现，必须在写的地方守住。
	mcpServersKey = "mcpServers"
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
	// Warnings 是不阻断写入、但需要让人知道的情况（如 .secrets/.gitignore 内容不符）。
	Warnings []string
}

// Writer 负责把下发内容落到本机。
type Writer struct {
	resolver *Resolver
	// home 是用户目录，用于给整文件落点加说明。
	home string
	// stateDir 是首次接管归档使用的代理状态目录，见 SetArchiveDir。
	stateDir string
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
		if kind := classifyTarget(t); kind != credNone {
			projectRoot := ""
			if t.Plan == proto.PlanProject {
				projectRoot = w.resolver.projects[t.ProjectID]
			}
			hash, warn, err := w.writeCredential(full, kind, f, projectRoot)
			res.Warnings = append(res.Warnings, warn...)
			if err != nil {
				return nil, err
			}
			paths = append(paths, full)
			lastHash = hash
			continue
		}
		if f.Format == proto.FormatManagedBlock {
			return nil, fmt.Errorf("%w: %s 格式只能用于 ssh 根的 config", ErrNotAllowed, proto.FormatManagedBlock)
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
		// 直接写 Content，不再拼说明行：说明已由服务端渲染时写入。
		//
		// 代理在这里加一行，会让"写下的字节"比"收到的字节"多一行，
		// 于是代理按写下的内容算哈希、服务端按下发的内容算哈希，
		// 两侧永远不等，每次对账都判成差异并重发（2026-10 的重写循环）。
		if err := writeFileAtomic(full, []byte(f.Content), mode); err != nil {
			return "", err
		}
		return hashOf([]byte(f.Content)), nil

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
		// 回执哈希必须与“下一轮对账读回来的哈希”完全同口径，否则
		// 服务端记下的落地哈希与代理上报的现场哈希不一致，
		// 每轮都会被判成本地改动（需求 6.3）。
		return HashFile(full, proto.FormatJSONKey, f.Key)
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
		if f.Format == proto.FormatManagedBlock {
			if classifyTarget(t) != credSSHConfig {
				continue
			}
			if err := removeManagedBlock(full); err != nil {
				return nil, err
			}
			paths = append(paths, full)
			continue
		}
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("删除 %q 失败: %w", full, err)
		}
		switch classifyTarget(t) {
		case credProjectSecret:
			removeEmptySecretsParents(full)
		case credNone:
			w.removeEmptyParents(full)
		}
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

// withNotice 只做幂等保护，不再拼说明行。
//
// 说明行由服务端渲染时写进 Content（见 render.RenderDevice）。代理这里
// 保留一层兜底：若收到的是旧服务端下发的内容（不含说明），代理也不会补，
// 因为补了就会让"写下的字节"与"收到的字节"不一致，回到重写循环。
//
// 换句话说：这里存在的唯一目的是让人看到"说明行不在这里加"这句话有个落点，
// 而不是留一个可以再加一行的地方。
func (w *Writer) withNotice(full, content string, secret bool) string {
	return content
}

// hashOf 计算内容哈希，与服务端同一算法（ssot.SumHex）。
func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// HashFile 读取落点并计算哈希，用于上报本地清单（需求 2.1 的第五个原语）。
//
// json_key 格式的落点只算 Dec 管理的那个条目：文件里其它键由用户维护，
// 把它们算进哈希会让每一次用户改动都被误判成“Dec 落点被改”。
//
// 条目按规范化后的语义算（proto.NormalizeJSONEntry），不带容器外壳。
// 服务端渲染同一份内容时用同一个函数，两侧因此可以逐字节对齐。
// 早期版本这里算的是「容器内全部 dec- 前缀条目」的序列化结果，
// 与服务端算的「单个条目原始字节」口径不同，导致 drift 长期恒为 1。
func HashFile(full string, format proto.Format, key string) (string, error) {
	data, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if format == proto.FormatJSONKey && key != "" {
		entry, ok := managedEntry(full, data, key)
		if !ok {
			// 文件在、但条目没了：返回空哈希表示“我保证不了这条的状态”。
			// 服务端据此补发，而不是把它当成“内容一致”跳过。
			return "", nil
		}
		norm, err := proto.NormalizeJSONEntry(entry)
		if err != nil {
			return "", err
		}
		return hashOf(norm), nil
	}
	if format == proto.FormatManagedBlock {
		return hashManagedBlock(data)
	}
	return hashOf(data), nil
}

// managedEntry 从 JSON 内容里取出指定键的条目原始字节。
//
// 只看 mcpServers：顶层不属于 Dec 的地盘，用户在那里放什么
// （包括其它工具的配置）都不该被算成“Dec 落点被改”。
func managedEntry(full string, data []byte, key string) ([]byte, bool) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, false
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := root[mcpServersKey]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, false
		}
	}
	entry, ok := servers[key]
	return entry, ok
}

// mergeJSONKey 把一个键合并进共享 JSON 配置文件（需求 6.3）。
//
// 只动目标键，其它键按原始字节回写：这样用户自己的 MCP 配置即使格式很怪，
// 也不会因为我们的反序列化而变形。文件不存在时新建。
//
// 条目必须落在 mcpServers 容器里。这个位置不是可选风格：四个 JSON 宿主
// （cursor / claude / codebuddy / with）都只读 mcpServers 下的键，写在顶层
// 的文件仍然是合法 JSON，所以不报错、不告警，只是那条 MCP 永远不会被加载
// ——失败是静默的，比写坏文件更难发现。
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

	servers := map[string]json.RawMessage{}
	if raw, ok := root[mcpServersKey]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			// mcpServers 存在但不是对象：这是用户的文件坏了。
			// 覆盖它会连带丢掉里面原有的服务器定义，所以同样拒绝。
			return nil, fmt.Errorf("%w: %q 的 mcpServers 不是 JSON 对象，拒绝改写", ErrNotAllowed, full)
		}
	}

	// 值必须是合法 JSON：云端给的是单条目对象。
	var probe any
	if err := json.Unmarshal([]byte(value), &probe); err != nil {
		return nil, fmt.Errorf("%w: 条目内容不是合法 JSON", ErrNotAllowed)
	}
	servers[key] = json.RawMessage(value)

	inner, err := json.Marshal(servers)
	if err != nil {
		return nil, fmt.Errorf("序列化 mcpServers 失败: %w", err)
	}
	root[mcpServersKey] = inner
	// 顺手清掉旧版本误写在顶层的同名键，否则宿主读不到它、人却看得见，
	// 排查时会被当成“已经写了”。
	delete(root, key)
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
//
// 与 mergeJSONKey 对称：只认 mcpServers 里的键。旧版本曾把条目写到顶层，
// 那种残留在卸载时也要清掉，否则文件里会一直留着一个没人读的键。
func removeJSONKey(full, key string) error {
	existing, err := readFileOrEmpty(full)
	if err != nil || len(existing) == 0 {
		return err
	}
	root := map[string]json.RawMessage{}
	if err := json.Unmarshal(existing, &root); err != nil {
		return fmt.Errorf("%w: %q 不是合法 JSON，拒绝改写", ErrNotAllowed, full)
	}

	changed := false
	// 顶层残留：旧版本写错位置的键。
	if _, ok := root[key]; ok {
		delete(root, key)
		changed = true
	}

	servers := map[string]json.RawMessage{}
	if raw, ok := root[mcpServersKey]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("%w: %q 的 mcpServers 不是 JSON 对象，拒绝改写", ErrNotAllowed, full)
		}
		if _, ok := servers[key]; ok {
			delete(servers, key)
			inner, err := json.Marshal(servers)
			if err != nil {
				return fmt.Errorf("序列化 mcpServers 失败: %w", err)
			}
			root[mcpServersKey] = inner
			changed = true
		}
	}
	if !changed {
		return nil
	}

	// 只剩空壳时把整个文件删掉，避免留下无意义的空配置。
	// 但必须先确认里面没有别人的键：mcpServers 已经清空时不算别人的。
	onlyEmptyServers := true
	for k, v := range root {
		if k == noticeKey {
			continue
		}
		if k == mcpServersKey {
			var left map[string]json.RawMessage
			if err := json.Unmarshal(v, &left); err != nil {
				onlyEmptyServers = false
				break
			}
			if len(left) != 0 {
				onlyEmptyServers = false
				break
			}
			continue
		}
		onlyEmptyServers = false
		break
	}
	if onlyEmptyServers {
		return os.Remove(full)
	}
	return writeFileAtomic(full, marshalIndentJSON(root), 0o644)
}

// VerifyWithin 校验一个绝对路径是否在允许的落脚范围内，供删除回执使用。
func (w *Writer) VerifyWithin(p string) error {
	if !isWithin(w.home, p) {
		return fmt.Errorf("%w: 路径 %q 不在用户目录内", ErrNotAllowed, p)
	}
	return nil
}
