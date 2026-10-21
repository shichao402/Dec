// Package prune 为本地剪枝提供可恢复的隔离区。
//
// pull 是本机同步链路里唯一会删除本地文件的环节。远端 Bitwarden 一侧的删除
// 进回收站，本地镜像侧此前却是直接 os.Remove——恰恰本地这份才是用户唯一没有
// 备份的地方。隔离区把「删」换成「移走并在保留期内可取回」，任何调用点都不得
// 退化为永久删除。
package prune

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// 默认策略。保留期与容量阈值都可通过 Store 的显式配置覆盖。
const (
	// DefaultRetention 是隔离项从移入起算的默认保留时长。
	DefaultRetention = 14 * 24 * time.Hour
	// DefaultMaxBytes 是隔离区默认容量上限，超出后按最早移入优先清理。
	DefaultMaxBytes int64 = 256 << 20
)

// manifestFileName 是每个隔离目录下的清单文件名。
const manifestFileName = "manifest.json"

// ErrUnwritable 表示隔离区不可写。调用方必须据此中止剪枝，禁止退化为永久删除。
var ErrUnwritable = errors.New("剪枝隔离区不可写")

// ErrNotFound 表示清单中不存在匹配的隔离项。
var ErrNotFound = errors.New("隔离项不存在")

// ErrConflict 表示恢复目标路径已被占用。
var ErrConflict = errors.New("恢复目标路径已存在")

// ErrExpired 表示隔离项已超过保留期，不可恢复。
var ErrExpired = errors.New("隔离项已超过保留期")

// Reason 说明一条本地条目为什么被判定为可剪枝。
type Reason string

const (
	// ReasonRemoteGone 表示远端已无对应条目，且本地内容可由远端状态还原。
	ReasonRemoteGone Reason = "remote_gone"
	// ReasonDerivedBundle 表示条目属于由远端派生的 bundle 目录，可重建。
	ReasonDerivedBundle Reason = "derived_bundle"
	// ReasonSSHLanding 表示条目是 Dec 托管落地的 SSH Key，可由远端重新落地。
	ReasonSSHLanding Reason = "ssh_landing"
)

// Entry 是隔离区中的一条记录，同时作为清单的持久化形态。
type Entry struct {
	// ID 是隔离项标识，由移入时间与原始路径确定，稳定且可重现。
	ID string `json:"id"`
	// OriginalPath 是移入前的绝对路径。
	OriginalPath string `json:"original_path"`
	// DisplayPath 是给人看的路径（项目相对或 ~/.dec/secrets 前缀形式）。
	DisplayPath string `json:"display_path"`
	// RelPath 是相对同步根的路径，恢复时用于回位。
	RelPath string `json:"rel_path"`
	// Kind 区分条目类型：note、ssh_key、dir。
	Kind string `json:"kind"`
	// Reason 是判定为可剪枝的理由，供用户事后判断这次删除是否合理。
	Reason Reason `json:"reason"`
	// Detail 是补充说明，例如命中了哪条规则。
	Detail string `json:"detail"`
	// Bundle 是所属项目或 bundle 名。
	Bundle string `json:"bundle"`
	// Plane 是来源平面：project 或 user。
	Plane string `json:"plane"`
	// ProjectRoot 是恢复时定位项目根的线索，user 平面为空。
	ProjectRoot string `json:"project_root,omitempty"`
	// QuarantinedAt 是移入时间。
	QuarantinedAt time.Time `json:"quarantined_at"`
	// ExpiresAt 是保留期截止时间。
	ExpiresAt time.Time `json:"expires_at"`
	// IsDir 标记这次移入的是整个目录。
	IsDir bool `json:"is_dir"`
	// SizeBytes 是移入前的占用大小，用于容量清理。
	SizeBytes int64 `json:"size_bytes"`
	// StoredRel 是隔离副本在该隔离目录内的相对路径。
	StoredRel string `json:"stored_rel"`
	// Reason 之外的调用方自定义标记，例如「中断时留下」。
	Note string `json:"note,omitempty"`
}

// Expired 报告条目是否已过保留期。
func (e Entry) Expired(now time.Time) bool {
	return !e.ExpiresAt.IsZero() && !now.Before(e.ExpiresAt)
}

// Options 配置隔离区策略。零值即取默认值。
type Options struct {
	// Retention 为零时使用 DefaultRetention，负值视为立即过期（仅供测试）。
	Retention time.Duration
	// MaxBytes 为零时使用 DefaultMaxBytes，负值表示不限容量。
	MaxBytes int64
	// Now 供测试注入时钟。
	Now func() time.Time
}

func (o Options) retention() time.Duration {
	if o.Retention == 0 {
		return DefaultRetention
	}
	return o.Retention
}

func (o Options) maxBytes() int64 {
	if o.MaxBytes == 0 {
		return DefaultMaxBytes
	}
	return o.MaxBytes
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// 环境变量名与 app 包保持一致：保留期、容量上限。
const (
	envRetention = "DEC_PRUNE_RETENTION"
	envMaxBytes  = "DEC_PRUNE_MAX_BYTES"
)

// OptionsFromEnv 从环境变量构造策略。lookup 便于测试注入。
//
// 解析失败时返回错误而不是忽略：一个写错的保留期如果被静默当成默认值，
// 用户会以为自己设的保护比实际更严。
func OptionsFromEnv(lookup func(string) (string, bool)) (Options, error) {
	opts := Options{}
	if lookup == nil {
		return opts, nil
	}
	if raw, ok := lookup(envRetention); ok && strings.TrimSpace(raw) != "" {
		d, err := parseRetention(strings.TrimSpace(raw))
		if err != nil {
			return opts, fmt.Errorf("%s=%q 无法解析: %w", envRetention, raw, err)
		}
		opts.Retention = d
	}
	if raw, ok := lookup(envMaxBytes); ok && strings.TrimSpace(raw) != "" {
		n, err := parseBytes(strings.TrimSpace(raw))
		if err != nil {
			return opts, fmt.Errorf("%s=%q 无法解析: %w", envMaxBytes, raw, err)
		}
		opts.MaxBytes = n
	}
	return opts, nil
}

// parseRetention 接受 Go duration 或纯小时数。
func parseRetention(raw string) (time.Duration, error) {
	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return 0, errors.New("保留期必须为正")
		}
		return d, nil
	}
	hours, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, errors.New("需为时长（如 336h）或小时数（如 336）")
	}
	if hours <= 0 {
		return 0, errors.New("保留期必须为正")
	}
	return time.Duration(hours * float64(time.Hour)), nil
}

// parseBytes 接受纯字节数或带 K/M/G（含 KiB/MiB/GiB）后缀的写法。
func parseBytes(raw string) (int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, errors.New("不能为空")
	}
	lower := strings.ToLower(trimmed)
	multiplier := int64(1)
	for _, suffix := range []struct {
		names []string
		mult  int64
	}{
		{[]string{"gib", "gb", "g"}, 1 << 30},
		{[]string{"mib", "mb", "m"}, 1 << 20},
		{[]string{"kib", "kb", "k"}, 1 << 10},
		{[]string{"b"}, 1},
	} {
		matched := false
		for _, name := range suffix.names {
			if strings.HasSuffix(lower, name) {
				multiplier = suffix.mult
				lower = strings.TrimSuffix(lower, name)
				matched = true
				break
			}
		}
		if matched {
			break
		}
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(lower), 64)
	if err != nil {
		return 0, errors.New("需为字节数或带 K/M/G 后缀的数值")
	}
	result := int64(value * float64(multiplier))
	if result < 0 {
		return -1, nil // 负值表示不限容量。
	}
	return result, nil
}

// Store 是隔离区的句柄。Root 是隔离根的绝对路径。
type Store struct {
	root string
	opts Options
}

// New 返回位于 root 的隔离区。不会创建目录，创建推迟到首次移动。
func New(root string, opts Options) *Store {
	return &Store{root: filepath.Clean(root), opts: opts}
}

// Root 返回隔离根目录。
func (s *Store) Root() string { return s.root }

// Retention 返回生效的保留期。
func (s *Store) Retention() time.Duration { return s.opts.retention() }

// MaxBytes 返回生效的容量上限，负值表示不限。
func (s *Store) MaxBytes() int64 { return s.opts.maxBytes() }

// Move 把 src 移入隔离区，记录 entry 并返回补全后的记录。
//
// 单条移动是原子的：同分区 rename，失败时原件保持不动。隔离区不可写时返回
// ErrUnwritable，调用方必须中止剪枝而不是删掉原件。
func (s *Store) Move(src string, entry Entry) (Entry, error) {
	src = filepath.Clean(src)
	info, err := os.Lstat(src)
	if err != nil {
		return Entry{}, err
	}

	now := s.opts.now().UTC()
	if entry.QuarantinedAt.IsZero() {
		entry.QuarantinedAt = now
	}
	if entry.ExpiresAt.IsZero() {
		entry.ExpiresAt = entry.QuarantinedAt.Add(s.opts.retention())
	}
	if entry.Kind == "" {
		if info.IsDir() {
			entry.Kind = "dir"
		} else {
			entry.Kind = "note"
		}
	}
	entry.IsDir = info.IsDir()
	entry.SizeBytes = measureSize(src, info)
	if entry.DisplayPath == "" {
		entry.DisplayPath = entry.OriginalPath
	}
	if entry.RelPath == "" {
		entry.RelPath = filepath.Base(src)
	}

	id := makeID(entry.QuarantinedAt, entry.OriginalPath)
	entry.ID = id

	dir := s.itemDir(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Entry{}, fmt.Errorf("%w: %v", ErrUnwritable, err)
	}

	storeName := filepath.Base(src)
	dest := filepath.Join(dir, storeName)
	// 同名冲突（两次剪同一路径的残留目录）不应让移动失败，改用 ID 兜底名。
	if _, statErr := os.Lstat(dest); statErr == nil {
		storeName = "content"
		dest = filepath.Join(dir, storeName)
		if _, again := os.Lstat(dest); again == nil {
			storeName = "content-" + hex.EncodeToString([]byte(id))[:8]
			dest = filepath.Join(dir, storeName)
		}
	}
	if err := relocate(src, dest, info.IsDir()); err != nil {
		return Entry{}, fmt.Errorf("%w: 移入隔离区失败: %v", ErrUnwritable, err)
	}
	entry.StoredRel = storeName

	if err := s.writeManifest(dir, entry); err != nil {
		// 清单写不进去说明隔离区实际不可用，把原件放回去，绝不留在半移动状态。
		if restoreErr := os.Rename(dest, src); restoreErr != nil {
			return Entry{}, fmt.Errorf("%w: 清单写入失败且回滚失败（原件在 %s）: %v", ErrUnwritable, dest, err)
		}
		_ = os.RemoveAll(dir)
		return Entry{}, fmt.Errorf("%w: %v", ErrUnwritable, err)
	}
	return entry, nil
}

// List 返回全部未过期与已过期的隔离项，按移入时间倒序。
// includeExpired 为 false 时过滤掉已过期项。
func (s *Store) List(includeExpired bool) ([]Entry, error) {
	entries, err := s.scan()
	if err != nil {
		return nil, err
	}
	now := s.opts.now().UTC()
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if !includeExpired && e.Expired(now) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].QuarantinedAt.Equal(out[j].QuarantinedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].QuarantinedAt.After(out[j].QuarantinedAt)
	})
	return out, nil
}

// Expiring 返回在 at 时刻之前（含）到期的隔离项。
//
// 这是清理前给用户看「将清理什么」的入口：传当前时间即得到已到期项，传未来
// 时间即得到届时会被清掉的项。注意它回答的是「到期时间早于 at」，不是
// 「at 时已经过期」——后者会让预览永远拿不到即将清理的清单。
func (s *Store) Expiring(at time.Time) ([]Entry, error) {
	entries, err := s.scan()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.ExpiresAt.IsZero() {
			continue
		}
		if !e.ExpiresAt.After(at) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExpiresAt.Before(out[j].ExpiresAt) })
	return out, nil
}

// Restore 把隔离项恢复到原始路径。目标已存在时返回 ErrConflict 且不覆盖。
func (s *Store) Restore(id string) (Entry, error) {
	entry, dir, err := s.find(id)
	if err != nil {
		return Entry{}, err
	}
	if entry.Expired(s.opts.now().UTC()) {
		return Entry{}, fmt.Errorf("%w: %s", ErrExpired, entry.DisplayPath)
	}
	stored := filepath.Join(dir, entry.StoredRel)
	if _, err := os.Lstat(stored); err != nil {
		return Entry{}, fmt.Errorf("隔离副本不可读 %s: %w", stored, err)
	}
	if _, err := os.Lstat(entry.OriginalPath); err == nil {
		return Entry{}, fmt.Errorf("%w: %s", ErrConflict, entry.DisplayPath)
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	}
	if err := os.MkdirAll(filepath.Dir(entry.OriginalPath), 0o755); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(stored, entry.OriginalPath); err != nil {
		return Entry{}, err
	}
	entry.Note = strings.TrimSpace(entry.Note + " | restored")
	if err := s.writeManifest(dir, entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// RestoreAll 批量恢复。任一条冲突不影响其余条目，返回已恢复项与逐条错误。
func (s *Store) RestoreAll(ids []string) ([]Entry, []error) {
	restored := make([]Entry, 0, len(ids))
	var errs []error
	for _, id := range ids {
		entry, err := s.Restore(id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		restored = append(restored, entry)
	}
	return restored, errs
}

// PurgeExpired 清理已过保留期的隔离项，返回被清理的条目。
func (s *Store) PurgeExpired() ([]Entry, error) {
	entries, err := s.scan()
	if err != nil {
		return nil, err
	}
	now := s.opts.now().UTC()
	purged := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if !e.Expired(now) {
			continue
		}
		if err := os.RemoveAll(s.itemDir(e.ID)); err != nil {
			return purged, err
		}
		purged = append(purged, e)
	}
	return purged, nil
}

// EnforceCapacity 在占用超过上限时按移入时间从早到晚清理，返回被清理的条目。
func (s *Store) EnforceCapacity() ([]Entry, error) {
	limit := s.opts.maxBytes()
	if limit < 0 {
		return nil, nil
	}
	entries, err := s.scan()
	if err != nil {
		return nil, err
	}
	var total int64
	for _, e := range entries {
		total += e.SizeBytes
	}
	if total <= limit {
		return nil, nil
	}
	// 最早移入的先走。
	sort.Slice(entries, func(i, j int) bool { return entries[i].QuarantinedAt.Before(entries[j].QuarantinedAt) })
	purged := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if total <= limit {
			break
		}
		if err := os.RemoveAll(s.itemDir(e.ID)); err != nil {
			return purged, err
		}
		total -= e.SizeBytes
		purged = append(purged, e)
	}
	return purged, nil
}

// ItemDir 返回隔离项的目录，供诊断与测试定位。
func (s *Store) ItemDir(id string) string { return s.itemDir(id) }

func (s *Store) itemDir(id string) string { return filepath.Join(s.root, id) }

func (s *Store) manifestPath(dir string) string { return filepath.Join(dir, manifestFileName) }

func (s *Store) writeManifest(dir string, entry Entry) error {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.manifestPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.manifestPath(dir))
}

func (s *Store) readManifest(dir string) (Entry, error) {
	data, err := os.ReadFile(s.manifestPath(dir))
	if err != nil {
		return Entry{}, err
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return Entry{}, fmt.Errorf("隔离清单损坏 %s: %w", dir, err)
	}
	return entry, nil
}

// scan 遍历隔离根。清单损坏的条目跳过而不使整次扫描失败——一份坏清单不能让
// 其余可恢复条目一起不可见。
func (s *Store) scan() ([]Entry, error) {
	dirents, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Entry, 0, len(dirents))
	for _, d := range dirents {
		if !d.IsDir() {
			continue
		}
		entry, err := s.readManifest(filepath.Join(s.root, d.Name()))
		if err != nil {
			continue
		}
		if entry.ID == "" {
			entry.ID = d.Name()
		}
		out = append(out, entry)
	}
	return out, nil
}

func (s *Store) find(id string) (Entry, string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Entry{}, "", fmt.Errorf("%w: 空 ID", ErrNotFound)
	}
	dir := s.itemDir(id)
	entry, err := s.readManifest(dir)
	if err != nil {
		return Entry{}, "", fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if entry.ID == "" {
		entry.ID = id
	}
	return entry, dir, nil
}

// relocate 把 src 移到 dest。同分区走 rename；跨分区（EXDEV）退化为复制后删除。
//
// 隔离区放在 ~/.dec 下，被剪条目可能在项目目录，两者常不在同一分区。此前
// 直接 rename 会让整条剪枝失败，剪枝就变成「静默不生效」——比报错更难发现。
func relocate(src, dest string, isDir bool) error {
	err := os.Rename(src, dest)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if copyErr := copyEntry(src, dest, isDir); copyErr != nil {
		// 复制失败时清理半成品，原件保持不动。
		_ = os.RemoveAll(dest)
		return copyErr
	}
	if removeErr := os.RemoveAll(src); removeErr != nil {
		return fmt.Errorf("复制成功但清理原件失败（原件仍在 %s）: %w", src, removeErr)
	}
	return nil
}

// copyEntry 复制文件或目录树，保留权限位与符号链接本身（不跟随）。
func copyEntry(src, dest string, isDir bool) error {
	if !isDir {
		return copyFile(src, dest)
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dest, rel)
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			link, linkErr := os.Readlink(path)
			if linkErr != nil {
				return linkErr
			}
			return os.Symlink(link, target)
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyFileWithInfo(path, target, info)
	})
}

func copyFile(src, dest string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	return copyFileWithInfo(src, dest, info)
}

func copyFileWithInfo(src, dest string, info os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// makeID 由移入时间与原始路径派生稳定 ID。同一路径在同一毫秒内被剪两次时
// 后者会写入同一个目录，因此附加纳秒位避免覆盖。
func makeID(at time.Time, originalPath string) string {
	h := sha256.Sum256([]byte(originalPath))
	return fmt.Sprintf("%s-%s", at.UTC().Format("20060102T150405.000000000Z"), hex.EncodeToString(h[:])[:10])
}

// measureSize 统计条目占用字节数。目录递归求和不含符号链接目标，避免把
// 链接指向的大文件计入容量清理判断。
func measureSize(path string, info os.FileInfo) int64 {
	if info.Mode()&os.ModeSymlink != 0 {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if fi, statErr := d.Info(); statErr == nil && !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	return total
}
