package legacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/sysproc"
)

// CommitSubject 是自动提交的固定说明（需求 8.2）。
const CommitSubject = "chore: 移除旧版 Dec 遗留文件"

// RepoStatus 是一个项目仓库的提交结果（需求 8.5）。
type RepoStatus string

const (
	RepoPushed    RepoStatus = "pushed"    // 已提交并推送
	RepoCommitted RepoStatus = "committed" // 已提交未推送
	RepoSkipped   RepoStatus = "skipped"   // 未提交
)

// RepoResult 是一个仓库的提交与推送结果。
type RepoResult struct {
	Repo   string     `json:"repo"`
	Paths  []string   `json:"paths"`
	Status RepoStatus `json:"status"`
	Commit string     `json:"commit,omitempty"`
	Reason string     `json:"reason,omitempty"`
	// Excluded 是删了但没有放进提交的被跟踪路径及原因。
	Excluded []string `json:"excluded,omitempty"`
	// Manual 是需要用户手工执行的命令。
	Manual []string `json:"manual,omitempty"`
}

// GitOptions 控制自动提交。
type GitOptions struct {
	// Disabled 为真时不做提交（测试与隔离实例用）。
	Disabled bool
	// NetTimeout 是 fetch、push 的超时，默认 60 秒。
	NetTimeout time.Duration
}

type gitRunner struct {
	dir     string
	timeout time.Duration
}

func (g gitRunner) run(net bool, args ...string) (string, error) {
	ctx := context.Background()
	if net {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.timeout)
		defer cancel()
	}
	cmd := sysproc.CommandContext(ctx, "git", append([]string{"-C", g.dir}, args...)...)
	// 不弹任何凭据或编辑器交互：这里没有终端可以回答。
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GCM_INTERACTIVE=never", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if ctx.Err() != nil {
			msg = "超时：" + msg
		}
		var ee *exec.ExitError
		if msg == "" && !errors.As(err, &ee) {
			msg = err.Error()
		}
		return out.String(), errors.New(firstLines(msg, 3))
	}
	return out.String(), nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " / ")
}

// commitProjects 把项目里被删、被改的被跟踪路径按仓库各提交一次并尝试推送（需求 8）。
func commitProjects(res *Result, opts GitOptions) []RepoResult {
	if opts.Disabled || res.Archive == nil {
		return nil
	}
	if opts.NetTimeout <= 0 {
		opts.NetTimeout = 60 * time.Second
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	shaOf := map[string]string{}
	for _, e := range res.Archive.Manifest.Entries {
		if e.Type == EntryFragment {
			shaOf[e.Path] = e.SHA256
		}
	}

	type cand struct {
		path  string
		strip bool
	}
	byRepo := map[string][]cand{}
	for _, r := range res.Items {
		if r.Item.Project == "" || (r.Status != StatusDeleted && r.Status != StatusStripped) {
			continue
		}
		top, err := gitRunner{dir: r.Item.Project, timeout: opts.NetTimeout}.run(false, "rev-parse", "--show-toplevel")
		if err != nil {
			continue // 不是 git 仓库，没有可提交的
		}
		top = strings.TrimSpace(top)
		byRepo[top] = append(byRepo[top], cand{path: r.Item.Path, strip: r.Status == StatusStripped})
	}

	repos := make([]string, 0, len(byRepo))
	for k := range byRepo {
		repos = append(repos, k)
	}
	sort.Strings(repos)
	var out []RepoResult
	for _, top := range repos {
		g := gitRunner{dir: top, timeout: opts.NetTimeout}
		rr := RepoResult{Repo: top}
		for _, c := range byRepo[top] {
			rel, err := relInRepo(top, c.path)
			if err != nil {
				continue
			}
			tracked, err := g.run(false, "ls-files", "-z", "--cached", "--", rel)
			if err != nil || tracked == "" {
				continue // 未被跟踪，删了就删了
			}
			if c.strip {
				if why := stripCommittable(g, rel, shaOf[c.path]); why != "" {
					rr.Excluded = append(rr.Excluded, rel+"："+why)
					continue
				}
			}
			rr.Paths = append(rr.Paths, rel)
		}
		if len(rr.Paths) == 0 && len(rr.Excluded) == 0 {
			continue
		}
		sort.Strings(rr.Paths)
		if len(rr.Paths) == 0 {
			rr.Status, rr.Reason = RepoSkipped, "没有可以单独提交的路径"
			out = append(out, rr)
			continue
		}
		commitAndPush(g, &rr, res.Archive.ID)
		out = append(out, rr)
	}
	return out
}

// relInRepo 把绝对路径换成仓库内相对路径。被删的路径本身已不存在，
// 所以按最近的现存上级做符号链接解析（macOS 的 /var 与 /private/var）。
func relInRepo(top, p string) (string, error) {
	realTop, err := filepath.EvalSymlinks(top)
	if err != nil {
		return "", err
	}
	anc, rest := p, ""
	for {
		if _, err := os.Lstat(anc); err == nil {
			break
		}
		parent := filepath.Dir(anc)
		if parent == anc {
			return "", fmt.Errorf("%s 没有现存的上级目录", p)
		}
		rest = filepath.Join(filepath.Base(anc), rest)
		anc = parent
	}
	realAnc, err := filepath.EvalSymlinks(anc)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realTop, filepath.Join(realAnc, rest))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s 不在仓库 %s 内", p, top)
	}
	return filepath.ToSlash(rel), nil
}

// stripCommittable 判断改写过的共享文件能否单独提交：
// 改写前的内容必须与 HEAD、暂存区一致，否则提交会把用户自己的改动一起带进去。
func stripCommittable(g gitRunner, rel, archivedSHA string) string {
	if archivedSHA == "" {
		return "找不到改写前的归档记录"
	}
	head, err := g.run(false, "show", "HEAD:"+rel)
	if err != nil {
		return "HEAD 里没有这个文件"
	}
	sum := sha256.Sum256([]byte(head))
	if hex.EncodeToString(sum[:]) != archivedSHA {
		return "文件里还有你未提交的改动，没有一起提交，请自行提交"
	}
	if _, err := g.run(false, "diff", "--cached", "--quiet", "--", rel); err != nil {
		return "这个文件有已暂存的改动，没有一起提交，请自行提交"
	}
	return ""
}

func commitAndPush(g gitRunner, rr *RepoResult, archiveID string) {
	body := "归档 id：" + archiveID + "\n由 dec-agent 旧版残留清理生成；可在本机工具页按该归档还原。"
	manualCommit := fmt.Sprintf("git -C %s commit -m %q -- %s", shellQuote(g.dir), CommitSubject, joinQuoted(rr.Paths))
	skip := func(why string) {
		rr.Status, rr.Reason = RepoSkipped, why
		rr.Manual = append(rr.Manual, manualCommit)
	}

	// 需求 8.4：进行中的操作、detached HEAD、没有身份时不提交。
	for marker, what := range map[string]string{
		"MERGE_HEAD": "merge", "CHERRY_PICK_HEAD": "cherry-pick", "REVERT_HEAD": "revert",
		"rebase-merge": "rebase", "rebase-apply": "rebase", "BISECT_LOG": "bisect",
	} {
		p, err := g.run(false, "rev-parse", "--git-path", marker)
		if err != nil {
			continue
		}
		p = strings.TrimSpace(p)
		if !filepath.IsAbs(p) {
			p = filepath.Join(g.dir, p)
		}
		if _, err := os.Lstat(p); err == nil {
			skip("仓库正在进行 " + what + "，没有提交；完成或中止后再手工提交")
			return
		}
	}
	branchRef, err := g.run(false, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		skip("处于 detached HEAD，没有提交")
		return
	}
	branch := strings.TrimPrefix(strings.TrimSpace(branchRef), "refs/heads/")
	for _, k := range []string{"user.name", "user.email"} {
		if v, err := g.run(false, "config", "--get", k); err != nil || strings.TrimSpace(v) == "" {
			skip("没有配置 git " + k + "，没有提交")
			return
		}
	}

	// 需求 8.1、8.6：路径限定提交，不加 --no-verify。钩子失败时 git 会还原暂存区。
	args := append([]string{"commit", "--quiet", "-m", CommitSubject, "-m", body, "--"}, rr.Paths...)
	if _, err := g.run(false, args...); err != nil {
		skip("提交失败（可能是提交钩子拒绝）：" + err.Error())
		return
	}
	head, _ := g.run(false, "rev-parse", "HEAD")
	rr.Commit = strings.TrimSpace(head)
	rr.Status = RepoCommitted

	// 需求 8.3：推送到上游，先 fetch，只在能快进时推。
	remote, _ := g.run(false, "config", "--get", "branch."+branch+".remote")
	merge, _ := g.run(false, "config", "--get", "branch."+branch+".merge")
	remote, merge = strings.TrimSpace(remote), strings.TrimSpace(merge)
	if remote == "" || merge == "" || remote == "." {
		rr.Reason = "当前分支没有远端上游，只提交未推送"
		return
	}
	manualPush := fmt.Sprintf("git -C %s push %s HEAD:%s", shellQuote(g.dir), remote, merge)
	if _, err := g.run(true, "fetch", "--quiet", "--", remote); err != nil {
		rr.Reason = "fetch 失败，没有推送：" + err.Error()
		rr.Manual = append(rr.Manual, manualPush)
		return
	}
	counts, err := g.run(false, "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	if err != nil {
		rr.Reason = "读取上游失败，没有推送：" + err.Error()
		rr.Manual = append(rr.Manual, manualPush)
		return
	}
	f := strings.Fields(counts)
	behind, _ := strconv.Atoi(first(f, 0))
	ahead, _ := strconv.Atoi(first(f, 1))
	switch {
	case behind > 0:
		rr.Reason = fmt.Sprintf("本地落后上游 %d 个提交，没有推送；请先 pull --rebase 再推送", behind)
		rr.Manual = append(rr.Manual, fmt.Sprintf("git -C %s pull --rebase", shellQuote(g.dir)), manualPush)
		return
	case ahead != 1:
		// 推送会把用户自己还没推的提交一起推上去，这不该由清理工具决定。
		rr.Reason = fmt.Sprintf("本地还有 %d 个你自己未推送的提交，没有替你推送", ahead-1)
		rr.Manual = append(rr.Manual, manualPush)
		return
	}
	if _, err := g.run(true, "push", "--quiet", "--", remote, "HEAD:"+merge); err != nil {
		rr.Reason = "推送失败：" + err.Error()
		rr.Manual = append(rr.Manual, manualPush)
		return
	}
	rr.Status = RepoPushed
}

func first(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t'\"$`\\") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func joinQuoted(ps []string) string {
	q := make([]string, len(ps))
	for i, p := range ps {
		q[i] = shellQuote(p)
	}
	return strings.Join(q, " ")
}
