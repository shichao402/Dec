package handler

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// SourceKind 对齐 Bitwarden 可同步条目类型（有限闭集）。
type SourceKind string

const (
	SourceNote SourceKind = "note"
	SourceSSH  SourceKind = "ssh_item"
)

// Item 是一次 Handler.Apply 的输入。
type Item struct {
	Source      SourceKind
	Name        string // 路由名：Note 用 basename；SSH 用逻辑名
	NoteContent string // SourceNote 时为正文
	ProjectRoot string
	BundleName  string
}

// Handler 按 SourceKind + 名字约定处理机器平面副作用。
type Handler interface {
	Kind() string
	Source() SourceKind
	Match(name string) bool
	Apply(ctx context.Context, item Item) error
}

// Registry 按源类型注册 Handler；Match 时后者覆盖前者同 Kind 亦可并存，按注册顺序找第一个 Match。
type Registry struct {
	mu       sync.RWMutex
	handlers map[SourceKind][]Handler
}

// NewRegistry 返回空注册表。
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[SourceKind][]Handler)}
}

// Register 追加一个 Handler。
func (r *Registry) Register(h Handler) {
	if h == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	src := h.Source()
	r.handlers[src] = append(r.handlers[src], h)
}

// Find 返回第一个 Match 成功的 Handler；无匹配返回 nil。
func (r *Registry) Find(source SourceKind, name string) Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, h := range r.handlers[source] {
		if h.Match(name) {
			return h
		}
	}
	return nil
}

var (
	defaultMu sync.RWMutex
	defaultR  = newDefaultRegistry()
)

func newDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(NewGitGCMHandler(nil))
	return r
}

// Default 返回进程级默认 Registry（含内置 gitgcm）。
func Default() *Registry {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultR
}

// SetDefault 替换默认 Registry（测试用）。
func SetDefault(r *Registry) (restore func()) {
	defaultMu.Lock()
	prev := defaultR
	if r == nil {
		defaultR = newDefaultRegistry()
	} else {
		defaultR = r
	}
	defaultMu.Unlock()
	return func() {
		defaultMu.Lock()
		defaultR = prev
		defaultMu.Unlock()
	}
}

// NoteRouteName 从 SyncTarget 相对路径取出路由名（basename）。
func NoteRouteName(relativePath string) string {
	return path.Base(filepath.ToSlash(strings.TrimSpace(relativePath)))
}

// ParseProcessorNoteName 解析 {实例}_{处理器}.yaml|.yml。
// 未匹配约定时 ok=false（不是错误）。
func ParseProcessorNoteName(name string) (instance, processor string, ok bool) {
	base := NoteRouteName(name)
	lower := strings.ToLower(base)
	var stem string
	switch {
	case strings.HasSuffix(lower, ".yaml"):
		stem = base[:len(base)-len(".yaml")]
	case strings.HasSuffix(lower, ".yml"):
		stem = base[:len(base)-len(".yml")]
	default:
		return "", "", false
	}
	idx := strings.LastIndex(stem, "_")
	if idx <= 0 || idx == len(stem)-1 {
		return "", "", false
	}
	instance = stem[:idx]
	processor = stem[idx+1:]
	if instance == "" || processor == "" {
		return "", "", false
	}
	if strings.ContainsAny(instance, `/\`) || strings.ContainsAny(processor, `/\`) {
		return "", "", false
	}
	return instance, processor, true
}

// ApplyNotes 对已落地（或待落地）的 Note 执行匹配到的 Handler。
// 未匹配则跳过。任一 Handler 失败则返回错误。
func ApplyNotes(ctx context.Context, reg *Registry, items []Item) (applied []string, err error) {
	if reg == nil {
		reg = Default()
	}
	for _, item := range items {
		if item.Source == "" {
			item.Source = SourceNote
		}
		if item.Source != SourceNote {
			return applied, fmt.Errorf("ApplyNotes 仅接受 SourceNote，收到 %s", item.Source)
		}
		name := NoteRouteName(item.Name)
		item.Name = name
		h := reg.Find(SourceNote, name)
		if h == nil {
			continue
		}
		if err := h.Apply(ctx, item); err != nil {
			return applied, fmt.Errorf("handler %s (%s): %w", h.Kind(), name, err)
		}
		applied = append(applied, name)
	}
	return applied, nil
}
