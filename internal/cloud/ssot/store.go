// Package ssot 的存储层：用 go-git 管理服务端内部的 bare 仓库。
//
// 这一层刻意做得很薄：没有分支、没有远程、没有对外协议。
// 仓库只被服务进程自己写入，使用者感知不到 git 的存在（已定决策 6）。
//
// 所有写入都经过 Store.Update：拿全局写锁 → 读工作树 → 交给回调修改 → 提交。
// 失败时不留下半个 commit。
package ssot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/yaml.v3"
)

var (
	// ErrNotFound 表示仓库里没有该对象。
	ErrNotFound = errors.New("not found")
	// ErrRevisionMismatch 表示写入方基于的 revision 已过期（乐观并发控制）。
	ErrRevisionMismatch = errors.New("revision mismatch")
	// ErrConflict 表示同名托管条目冲突。
	ErrConflict = errors.New("conflict")
)

// Author 描述一次提交的来源，用于审计和吊销后的轮换清单。
type Author struct {
	// Name 是提交者标识，通常是设备 ID 或 "cloud"。
	Name string
	// Email 是占位邮箱，git 需要一个。
	Email string
	// Source 是请求来源，会写进 commit message 的 trailer。
	Source string
}

// UpdateFunc 在持有写锁的情况下修改工作树。
//
// 回调只能通过提供的 Tx 修改内容；返回错误时不提交。
// 回调内不要做网络调用，避免长时间持锁。
type UpdateFunc func(tx *Tx) (message string, err error)

// Store 是云端 SSOT 仓库。
type Store struct {
	dir  string
	repo *git.Repository

	// mu 让资产写入和密钥写入串行执行（需求 7.6）。
	// 单人使用，一把全局锁足够，而且比不变量更容易推理。
	mu sync.Mutex

	// head 是最近一次提交的 revision，读路径用它做快速判断。
	headMu sync.RWMutex
	head   string
}

// InitRepo 初始化一个工作树仓库，并把初始分支设为 main。
//
// 为什么不用 git.PlainInit：它的默认分支是 master（go-git 不读
// init.defaultBranch）。本地仓库叫 master、远端仓库叫 main 时，推送要么
// 找不到分支，要么在远端另起一条 master，与默认分支分叉——两种结果都让
// 冷备看起来"配好了"，实际上什么也没备上去。
//
// 显式对齐到 main 是唯一不依赖环境配置的写法：远端仓库由 GitHub 创建，
// 默认分支就是 main。
func InitRepo(dir string) (*git.Repository, error) {
	return git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
}

// Open 打开或初始化一个 SSOT 仓库。
//
// 首次打开时创建初始 commit，让 revision 从第一刻起就有意义。
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 SSOT 目录失败: %w", err)
	}
	repo, err := git.PlainOpen(dir)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		repo, err = InitRepo(dir)
	}
	if err != nil {
		return nil, fmt.Errorf("打开 SSOT 仓库失败: %w", err)
	}
	s := &Store{dir: dir, repo: repo}
	if err := s.ensureInitialCommit(); err != nil {
		return nil, err
	}
	if err := s.refreshHead(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir 返回仓库工作树路径。
func (s *Store) Dir() string { return s.dir }

// Head 返回当前 revision。
func (s *Store) Head() string {
	s.headMu.RLock()
	defer s.headMu.RUnlock()
	return s.head
}

func (s *Store) refreshHead() error {
	ref, err := s.repo.Head()
	if err != nil {
		return fmt.Errorf("读取 HEAD 失败: %w", err)
	}
	s.headMu.Lock()
	s.head = ref.Hash().String()
	s.headMu.Unlock()
	return nil
}

func (s *Store) ensureInitialCommit() error {
	if _, err := s.repo.Head(); err == nil {
		return nil
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return fmt.Errorf("读取 HEAD 失败: %w", err)
	}

	gitignore := []byte("*.tmp\n.rootkey*\n")
	if err := os.WriteFile(filepath.Join(s.dir, ".gitignore"), gitignore, 0o600); err != nil {
		return fmt.Errorf("写入 .gitignore 失败: %w", err)
	}
	for _, sub := range []string{DirAssets, DirSecrets, DirConfig} {
		if err := os.MkdirAll(filepath.Join(s.dir, filepath.FromSlash(sub)), 0o700); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", sub, err)
		}
		if err := os.WriteFile(filepath.Join(s.dir, filepath.FromSlash(sub), ".keep"), nil, 0o600); err != nil {
			return fmt.Errorf("写入 %s/.keep 失败: %w", sub, err)
		}
	}
	if err := s.writeJSONFile(FileDevices, DeviceTable{SchemaVersion: SchemaVersion}); err != nil {
		return err
	}
	if err := s.writeJSONFile(FileProjects, ProjectTable{SchemaVersion: SchemaVersion}); err != nil {
		return err
	}
	if err := s.commitAll(Author{Name: "cloud", Email: "cloud@dec.local", Source: "init"}, "初始化 SSOT 仓库"); err != nil {
		return err
	}
	return nil
}

// Update 在写锁内修改工作树并提交。
//
// 回调返回空 message 时视为无变更，直接返回当前 revision。
func (s *Store) Update(author Author, fn UpdateFunc) (string, error) {
	if author.Name == "" {
		author.Name = "cloud"
	}
	if author.Email == "" {
		author.Email = author.Name + "@dec.local"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx := &Tx{store: s, staged: map[string]bool{}}
	message, err := fn(tx)
	if err != nil {
		// 回调失败时要回滚它可能已经写下的部分文件，保持工作树干净。
		_ = tx.rollback()
		return "", err
	}
	if len(tx.staged) == 0 {
		return s.Head(), nil
	}
	if message == "" {
		message = "更新 SSOT"
	}
	if err := s.commitStaged(author, message, tx.staged); err != nil {
		_ = tx.rollback()
		return "", err
	}
	if err := s.refreshHead(); err != nil {
		return "", err
	}
	return s.Head(), nil
}

// ReadFile 读取仓库内某个文件。路径必须是规范的仓库相对路径。
func (s *Store) ReadFile(rel string) ([]byte, error) {
	if err := SafeRelPath(rel); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.dir, filepath.FromSlash(rel)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, rel)
		}
		return nil, err
	}
	return data, nil
}

// ListDir 列出仓库内某个目录下的直接子项（文件名或目录名）。
func (s *Store) ListDir(rel string) ([]string, error) {
	if rel != "" {
		if err := SafeRelPath(rel); err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(filepath.Join(s.dir, filepath.FromSlash(rel)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, rel)
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Name() == ".keep" || e.Name() == ".git" {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// WalkFiles 遍历仓库内某个目录下的全部文件，返回仓库相对路径。
func (s *Store) WalkFiles(rel string) ([]string, error) {
	root := filepath.Join(s.dir, filepath.FromSlash(rel))
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == ".keep" {
			return nil
		}
		relPath, rerr := filepath.Rel(s.dir, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(relPath))
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, rel)
		}
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// ReadJSON 读取并解析一个 JSON 文件。
func ReadJSON[T any](s *Store, rel string) (T, error) {
	var zero T
	data, err := s.ReadFile(rel)
	if err != nil {
		return zero, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, fmt.Errorf("解析 %s 失败: %w", rel, err)
	}
	return v, nil
}

// ReadYAML 读取并解析一个 YAML 文件，未知字段直接报错。
func ReadYAML[T any](s *Store, rel string) (T, error) {
	var zero T
	data, err := s.ReadFile(rel)
	if err != nil {
		return zero, err
	}
	var v T
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&v); err != nil {
		return zero, fmt.Errorf("解析 %s 失败: %w", rel, err)
	}
	return v, nil
}

func (s *Store) writeJSONFile(rel string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 %s 失败: %w", rel, err)
	}
	data = append(data, '\n')
	full := filepath.Join(s.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o600)
}

// Tx 是一次事务内可用的工作树操作。
type Tx struct {
	store  *Store
	staged map[string]bool
	// touched 记录被改动或删除的文件，便于失败回滚。
	touched map[string][]byte
}

// Revision 返回事务开始时的 revision，用于乐观并发控制。
func (t *Tx) Revision() string { return t.store.Head() }

// EnsureRevision 校验调用方给出的 base revision 是否仍是最新。
// base 为空表示不做校验。
func (t *Tx) EnsureRevision(base string) error {
	if base == "" {
		return nil
	}
	if base != t.store.Head() {
		return fmt.Errorf("%w: 期望 %s，当前 %s", ErrRevisionMismatch, base, t.store.Head())
	}
	return nil
}

// WriteFile 写入仓库内一个文件并暂存。
func (t *Tx) WriteFile(rel string, data []byte, mode fs.FileMode) error {
	if err := SafeRelPath(rel); err != nil {
		return err
	}
	full := filepath.Join(t.store.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	t.remember(rel, full)
	if mode == 0 {
		mode = 0o600
	}
	if err := os.WriteFile(full, data, mode); err != nil {
		return err
	}
	t.staged[rel] = true
	return nil
}

// WriteJSON 写入 JSON 文件并暂存。
func (t *Tx) WriteJSON(rel string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 %s 失败: %w", rel, err)
	}
	return t.WriteFile(rel, append(data, '\n'), 0o600)
}

// DeleteFile 删除仓库内一个文件并暂存删除。
func (t *Tx) DeleteFile(rel string) error {
	if err := SafeRelPath(rel); err != nil {
		return err
	}
	full := filepath.Join(t.store.dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // 已经不存在，视为成功，保持幂等
		}
		return err
	}
	t.remember(rel, full)
	if err := os.Remove(full); err != nil {
		return err
	}
	t.staged[rel] = true
	_ = data
	return nil
}

// DeleteDir 删除一个目录及其内容并暂存。
func (t *Tx) DeleteDir(rel string) error {
	if err := SafeRelPath(rel); err != nil {
		return err
	}
	files, err := t.store.WalkFiles(rel)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	for _, f := range files {
		if err := t.DeleteFile(f); err != nil {
			return err
		}
	}
	full := filepath.Join(t.store.dir, filepath.FromSlash(rel))
	if err := os.RemoveAll(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	t.staged[rel] = true
	return nil
}

// ReadFile 在事务内读取文件。
func (t *Tx) ReadFile(rel string) ([]byte, error) { return t.store.ReadFile(rel) }

// Store 返回底层只读访问，供回调里做列表等只读操作。
func (t *Tx) Store() *Store { return t.store }

// remember 记录文件改动前的状态，用于失败回滚。
func (t *Tx) remember(rel, full string) {
	if t.touched == nil {
		t.touched = map[string][]byte{}
	}
	if _, ok := t.touched[rel]; ok {
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		t.touched[rel] = nil // nil 表示原先不存在
		return
	}
	t.touched[rel] = data
}

// rollback 把工作树恢复到事务开始前的状态。
func (t *Tx) rollback() error {
	for rel, data := range t.touched {
		full := filepath.Join(t.store.dir, filepath.FromSlash(rel))
		if data == nil {
			_ = os.Remove(full)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(full), 0o700)
		_ = os.WriteFile(full, data, 0o600)
	}
	return nil
}

// commitStaged 把暂存的文件提交进仓库。
func (s *Store) commitStaged(author Author, message string, staged map[string]bool) error {
	wt, err := s.repo.Worktree()
	if err != nil {
		return fmt.Errorf("获取工作树失败: %w", err)
	}
	for rel := range staged {
		// 目录被整体删除时，工作树里已经没有这个路径，wt.Add 会报 entry not found。
		// 这类变更是由 AddWithOptions{All:true} 统一收集的，因此只在路径仍存在时显式暂存。
		if _, statErr := os.Lstat(filepath.Join(s.dir, filepath.FromSlash(rel))); statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("检查 %s 失败: %w", rel, statErr)
		}
		if _, err := wt.Add(filepath.ToSlash(rel)); err != nil {
			return fmt.Errorf("暂存 %s 失败: %w", rel, err)
		}
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return fmt.Errorf("暂存变更失败: %w", err)
	}
	if _, err := wt.Commit(withTrailer(message, author.Source), &git.CommitOptions{
		Author: &object.Signature{
			Name:  author.Name,
			Email: author.Email,
			When:  time.Now().UTC(),
		},
	}); err != nil {
		if errors.Is(err, git.ErrEmptyCommit) {
			return nil
		}
		return fmt.Errorf("提交失败: %w", err)
	}
	return nil
}

// commitAll 提交工作树中的全部改动，用于初始化。
func (s *Store) commitAll(author Author, message string) error {
	wt, err := s.repo.Worktree()
	if err != nil {
		return fmt.Errorf("获取工作树失败: %w", err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return fmt.Errorf("暂存变更失败: %w", err)
	}
	if _, err := wt.Commit(withTrailer(message, author.Source), &git.CommitOptions{
		Author: &object.Signature{Name: author.Name, Email: author.Email, When: time.Now().UTC()},
	}); err != nil {
		if errors.Is(err, git.ErrEmptyCommit) {
			return nil
		}
		return fmt.Errorf("提交失败: %w", err)
	}
	return nil
}

// withTrailer 在 commit message 末尾附上来源，便于审计回溯。
func withTrailer(message, source string) string {
	message = strings.TrimRight(message, "\n")
	if source == "" {
		return message
	}
	return message + "\n\nSource: " + source
}

// Commit 是一条历史提交的摘要。
type Commit struct {
	Hash    string
	Message string
	Author  string
	Source  string
	When    time.Time
}

// History 返回仓库的提交历史，最多 limit 条（0 表示不限）。
func (s *Store) History(ctx context.Context, limit int) ([]Commit, error) {
	iter, err := s.repo.Log(&git.LogOptions{})
	if err != nil {
		return nil, fmt.Errorf("读取历史失败: %w", err)
	}
	defer iter.Close()

	var out []Commit
	err = iter.ForEach(func(c *object.Commit) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		out = append(out, Commit{
			Hash:    c.Hash.String(),
			Message: stripTrailer(c.Message),
			Author:  c.Author.Name,
			Source:  parseTrailer(c.Message),
			When:    c.Author.When,
		})
		if limit > 0 && len(out) >= limit {
			return errStopIter
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopIter) {
		return nil, err
	}
	return out, nil
}

var errStopIter = errors.New("stop")

func parseTrailer(message string) string {
	const prefix = "Source: "
	for _, line := range strings.Split(message, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func stripTrailer(message string) string {
	idx := strings.LastIndex(message, "\n\nSource: ")
	if idx < 0 {
		return strings.TrimRight(message, "\n")
	}
	return strings.TrimRight(message[:idx], "\n")
}

// FileAt 返回某个 commit 中某个文件的字节内容。
func (s *Store) FileAt(commit, rel string) ([]byte, error) {
	if err := SafeRelPath(rel); err != nil {
		return nil, err
	}
	c, err := s.repo.CommitObject(plumbing.NewHash(commit))
	if err != nil {
		return nil, fmt.Errorf("%w: commit %s", ErrNotFound, commit)
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	f, err := tree.File(rel)
	if err != nil {
		return nil, fmt.Errorf("%w: %s@%s", ErrNotFound, rel, commit)
	}
	content, err := f.Contents()
	if err != nil {
		return nil, err
	}
	return []byte(content), nil
}

// HashFile 返回一段内容的稳定哈希，用于下发对账。
func HashFile(data []byte) string { return SumHex(data) }
