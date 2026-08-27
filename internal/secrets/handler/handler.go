package handler

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/shichao402/Dec/internal/secrets"
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
	Name        string // 相对同步根路径（完整路径，如 .gcm/cnb.yaml）
	NoteContent string // SourceNote 时为正文
	ProjectRoot string
	BundleName  string
	// ProjectScoped 仅用于 ADR 0016 private/project：副作用必须绑定当前工作区。
	// false 保持历史 bundle/user 的机器级行为。
	ProjectScoped bool
}

// Handler 按 SourceKind + 名字约定处理机器平面副作用。
type Handler interface {
	Kind() string
	Source() SourceKind
	Match(name string) bool
	Apply(ctx context.Context, item Item) error
	// Revoke 撤销 Apply 造成的机器平面副作用（删除 Secure Note 时调用）。
	Revoke(ctx context.Context, item Item) error
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
// name 应为完整相对路径（点类型目录路由依赖路径首段）。
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
	r.Register(NewGCMHandler(nil))
	return r
}

// Default 返回进程级默认 Registry（含内置 gcm）。
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

// NormalizeNotePath 规范化相对同步根路径（slash）。
func NormalizeNotePath(relativePath string) string {
	return path.Clean(filepath.ToSlash(strings.TrimSpace(relativePath)))
}

// NoteRouteName 从 SyncTarget 相对路径取出 basename（遗留辅助；路由请用完整路径）。
func NoteRouteName(relativePath string) string {
	return path.Base(NormalizeNotePath(relativePath))
}

// ParseProcessorNoteName 解析旧约定 {实例}_{处理器}.yaml|.yml（仅迁移器用）。
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

// MatchGCMPath 判断相对路径是否由 gcm 处理器处理（.gcm/...）。
func MatchGCMPath(name string) bool {
	tp, ok, err := secrets.ParseTypePath(name)
	return err == nil && ok && tp.Type.ID == secrets.SecretTypeGCM
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
		name := NormalizeNotePath(item.Name)
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

// RevokeNotes 对匹配到 Handler 的 Note 执行撤销（与 ApplyNotes 对称）。
// 未匹配则跳过。任一 Handler 失败则返回错误。
func RevokeNotes(ctx context.Context, reg *Registry, items []Item) (revoked []string, err error) {
	if reg == nil {
		reg = Default()
	}
	for _, item := range items {
		if item.Source == "" {
			item.Source = SourceNote
		}
		if item.Source != SourceNote {
			return revoked, fmt.Errorf("RevokeNotes 仅接受 SourceNote，收到 %s", item.Source)
		}
		name := NormalizeNotePath(item.Name)
		item.Name = name
		h := reg.Find(SourceNote, name)
		if h == nil {
			continue
		}
		if err := h.Revoke(ctx, item); err != nil {
			return revoked, fmt.Errorf("handler %s (%s): %w", h.Kind(), name, err)
		}
		revoked = append(revoked, name)
	}
	return revoked, nil
}
