package ssot

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/shichao402/Dec/internal/sysproc"
)

// 历史保留与回收（ADR 0061）。
//
// 本机 SSOT 只保留最近 HistoryRetentionDays 天的历史，更早的提交由冷备仓库
// （shichao402/dec-cloud-backup）完整保存。做法是把本机仓库变成 shallow：
// 在保留期起点写 .git/shallow，再让 git 回收边界之前的对象。
//
// 这不是改写历史（需求 1.4）：没有任何提交被重写，哈希全部不变，冷备远端
// 仍是只追加的完整链，推送前的祖先校验（需求 15.2）照常成立。
// 本机只是不再持有旧对象，相当于一次"本地遗忘、远端存档"。

// HistoryRetentionDays 是本机 SSOT 保留的历史天数。
const HistoryRetentionDays = 180

// HistoryRetention 是 HistoryRetentionDays 对应的时长。
const HistoryRetention = HistoryRetentionDays * 24 * time.Hour

// gcMinLooseObjects 是不截断时触发打包的松散对象阈值。
//
// SSOT 很小（线上 9 天 818 个松散对象、打包后 600K），每天跑也不贵；
// 设个阈值只为避免空闲时每天白白重写一次 pack。
const gcMinLooseObjects = 256

// GcResult 描述一次回收做了什么，供上报与测试观察。
type GcResult struct {
	// Truncated 表示本次把保留期之前的历史回收了。
	Truncated bool
	// Baseline 是回收后本机历史的起点提交；未截断时为空。
	Baseline string
	// Skipped 说明为什么没有截断（例如冷备还没覆盖到边界）；为空表示无需截断或已截断。
	Skipped string
	// Packed 表示执行了 git gc。
	Packed bool
}

// Gc 按保留期回收本机历史，并打包松散对象。
//
// 应在冷备推送成功之后调用：截断边界必须已在冷备里，否则回收等于丢数据。
func (s *Store) Gc() (*GcResult, error) {
	return s.GcAt(time.Now(), gcMinLooseObjects)
}

// GcAt 是可注入时间与阈值的 Gc，仅为可测。
//
// 全程持有写锁 mu 与 repoMu：SSOT 很小，回收在亚秒级，持锁换来的是
// 可以安全地用 --prune=now（不会有正在写入的对象被误删），以及读路径
// 永远不会看到被删的 pack。审计仓库不能这样做，因为它大且写入频繁。
func (s *Store) GcAt(now time.Time, minLoose int) (*GcResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repoMu.Lock()
	defer s.repoMu.Unlock()

	res := &GcResult{}
	baseline, skipped, err := s.retentionBoundaryLocked(now.Add(-HistoryRetention))
	if err != nil {
		return nil, err
	}
	res.Skipped = skipped

	if !baseline.IsZero() {
		if err := s.writeShallowLocked(baseline); err != nil {
			return nil, err
		}
		// reflog 里可能还引用着旧提交（例如曾用系统 git 操作过），不清掉就回收不了。
		if err := s.runGitLocked("reflog", "expire", "--expire=now", "--all"); err != nil {
			return nil, err
		}
		if err := s.runGitLocked("-c", "gc.auto=0", "gc", "--prune=now", "--quiet"); err != nil {
			return nil, err
		}
		res.Truncated = true
		res.Packed = true
		res.Baseline = baseline.String()
	} else {
		loose, err := looseObjects(filepath.Join(s.dir, ".git", "objects"))
		if err != nil {
			return nil, err
		}
		if loose >= minLoose {
			if err := s.runGitLocked("-c", "gc.auto=0", "gc", "--quiet"); err != nil {
				return nil, err
			}
			res.Packed = true
		}
	}

	if res.Packed {
		// gc 删掉了旧 pack，必须换句柄，否则后续读写会报 object not found。
		repo, err := git.PlainOpen(s.dir)
		if err != nil {
			return nil, fmt.Errorf("回收后重新打开 SSOT 仓库失败: %w", err)
		}
		s.repo = repo
	}
	return res, nil
}

// retentionBoundaryLocked 找出本次应作为新起点的提交。
//
// 起点是"保留期开始前的最后一个提交"：它的内容就是保留期开始时的完整状态，
// 留下它，保留期内第一个提交才有可比较的父提交，也才能回滚到"保留期开始时"。
//
// 返回零值哈希表示不截断，此时 skipped 可能说明原因。调用方须持有 repoMu。
func (s *Store) retentionBoundaryLocked(cutoff time.Time) (plumbing.Hash, string, error) {
	head, err := s.repo.Head()
	if err != nil {
		return plumbing.ZeroHash, "", fmt.Errorf("读取 HEAD 失败: %w", err)
	}
	shallow := s.shallowSetLocked()

	var boundary *plumbingCommit
	h := head.Hash()
	for {
		c, err := s.repo.CommitObject(h)
		if err != nil {
			return plumbing.ZeroHash, "", fmt.Errorf("读取提交 %s 失败: %w", h, err)
		}
		if c.Author.When.Before(cutoff) {
			boundary = &plumbingCommit{hash: h, parents: c.NumParents()}
			break
		}
		if shallow[h] || c.NumParents() == 0 {
			return plumbing.ZeroHash, "", nil // 全部历史都在保留期内
		}
		h = c.ParentHashes[0]
	}
	if shallow[boundary.hash] || boundary.parents == 0 {
		return plumbing.ZeroHash, "", nil // 边界之前已经没有东西可回收
	}

	// 安全条件：被回收的提交必须都已在冷备里。等价于边界是冷备远端 head
	// 的祖先（或就是它）。冷备未配置、从未推送成功、或推送落后于边界时都不截断。
	archive, err := s.archiveHeadLocked(head.Name())
	if err != nil {
		return plumbing.ZeroHash, "", err
	}
	if archive.IsZero() {
		return plumbing.ZeroHash, "冷备未配置或尚未推送成功，保留全部历史", nil
	}
	h = archive
	for {
		if h == boundary.hash {
			return boundary.hash, "", nil
		}
		c, err := s.repo.CommitObject(h)
		if err != nil {
			return plumbing.ZeroHash, "", fmt.Errorf("读取冷备提交 %s 失败: %w", h, err)
		}
		if shallow[h] || c.NumParents() == 0 {
			break
		}
		h = c.ParentHashes[0]
	}
	return plumbing.ZeroHash, "冷备还没有覆盖到保留期边界，暂不回收", nil
}

type plumbingCommit struct {
	hash    plumbing.Hash
	parents int
}

// archiveHeadLocked 返回冷备远端（origin）在当前分支上的 head。
//
// origin 由冷备推送（backup.Pusher.EnsureRemote）配置，推送前会 fetch，
// 所以这个跟踪引用就是"冷备里确定已有的最新提交"。
func (s *Store) archiveHeadLocked(branch plumbing.ReferenceName) (plumbing.Hash, error) {
	if !branch.IsBranch() {
		return plumbing.ZeroHash, nil
	}
	ref, err := s.repo.Reference(plumbing.NewRemoteReferenceName("origin", branch.Short()), true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return plumbing.ZeroHash, nil
		}
		return plumbing.ZeroHash, fmt.Errorf("读取冷备跟踪引用失败: %w", err)
	}
	return ref.Hash(), nil
}

// shallowSetLocked 读出当前 shallow 边界。调用方须持有 repoMu。
func (s *Store) shallowSetLocked() map[plumbing.Hash]bool {
	out := map[plumbing.Hash]bool{}
	hashes, err := s.repo.Storer.Shallow()
	if err != nil {
		return out
	}
	for _, h := range hashes {
		out[h] = true
	}
	return out
}

// writeShallowLocked 把边界写成唯一的 shallow 条目。
//
// 旧条目都是新边界的祖先，新边界生效后它们已不可达，留着反而误导。
// 写临时文件再改名，避免中途失败留下半截 shallow 文件。
func (s *Store) writeShallowLocked(h plumbing.Hash) error {
	path := filepath.Join(s.dir, ".git", "shallow")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(h.String()+"\n"), 0o600); err != nil {
		return fmt.Errorf("写入 shallow 失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("写入 shallow 失败: %w", err)
	}
	return nil
}

// runGitLocked 在仓库目录下执行系统 git。
//
// 用系统 git 而不是 go-git 的理由同审计仓库（ADR 0043 第五节）：go-git 不回收
// 松散对象，也不按 shallow 边界修剪。经 sysproc 创建，避免 Windows 弹控制台。
func (s *Store) runGitLocked(args ...string) error {
	cmd := sysproc.Command("git", args...)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=dec-cloud", "GIT_AUTHOR_EMAIL=cloud@dec.local",
		"GIT_COMMITTER_NAME=dec-cloud", "GIT_COMMITTER_EMAIL=cloud@dec.local",
	)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s 失败: %w（输出: %s）", strings.Join(args, " "), err, strings.TrimSpace(buf.String()))
	}
	return nil
}

// looseObjects 统计松散对象数量。
func looseObjects(objectsDir string) (int, error) {
	entries, err := os.ReadDir(objectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取对象目录失败: %w", err)
	}
	total := 0
	for _, de := range entries {
		name := de.Name()
		if !de.IsDir() || len(name) != 2 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(objectsDir, name))
		if err != nil {
			continue
		}
		total += len(files)
	}
	return total, nil
}

// RetainedSince 返回本机保留历史的起点时间。
//
// 已截断时是保留期起点提交的时间；从未截断时是根提交的时间。
func (s *Store) RetainedSince() (time.Time, bool, error) {
	s.repoMu.RLock()
	defer s.repoMu.RUnlock()
	head, err := s.repo.Head()
	if err != nil {
		return time.Time{}, false, err
	}
	shallow := s.shallowSetLocked()
	h := head.Hash()
	for {
		c, err := s.repo.CommitObject(h)
		if err != nil {
			return time.Time{}, false, err
		}
		if shallow[h] {
			return c.Author.When, true, nil
		}
		if c.NumParents() == 0 {
			return c.Author.When, false, nil
		}
		h = c.ParentHashes[0]
	}
}
