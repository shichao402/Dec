package contribute

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/sysproc"
)

type Mode string

const (
	ModeAuto  Mode = "auto"
	ModePR    Mode = "pr"
	ModeIssue Mode = "issue"
)

type Options struct {
	OriginRepo string // owner/name
	Asset      string
	Title      string
	Body       string
	Diff       string
	Mode       Mode
	Branch     string
}

type Result struct {
	Kind string
	URL  string
}

func Propose(ctx context.Context, opts Options) (*Result, error) {
	mode := opts.Mode
	if mode == "" {
		mode = ModeAuto
	}
	repo := strings.TrimSpace(opts.OriginRepo)
	if repo == "" || !strings.Contains(repo, "/") {
		return nil, fmt.Errorf("需要 origin 仓库 owner/name")
	}
	if strings.TrimSpace(opts.Diff) == "" {
		return nil, fmt.Errorf("空 diff，拒绝提交")
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "dec: " + strings.TrimSpace(opts.Asset)
	}
	body := opts.Body
	if !strings.Contains(body, "```") {
		body += "\n\n```diff\n" + opts.Diff + "\n```\n"
	}
	switch mode {
	case ModeIssue:
		return createIssue(ctx, repo, title, body)
	case ModePR:
		return createPR(ctx, repo, title, body, opts.Branch)
	case ModeAuto:
		if r, err := createPR(ctx, repo, title, body, opts.Branch); err == nil {
			return r, nil
		}
		return createIssue(ctx, repo, title, body)
	default:
		return nil, fmt.Errorf("未知 mode %q", mode)
	}
}

func DraftDir(projectRoot, provideKey string) string {
	return filepath.Join(projectRoot, ".dec", "drafts", provideKey)
}

func createIssue(ctx context.Context, repo, title, body string) (*Result, error) {
	out, err := gh(ctx, "issue", "create", "-R", repo, "-t", title, "-b", body, "-l", "dec-asset")
	if err != nil {
		// 第一次接入的提供方仓库通常还没有 dec-asset label；标签不应阻断贡献。
		out, err = gh(ctx, "issue", "create", "-R", repo, "-t", title, "-b", body)
		if err != nil {
			return nil, err
		}
	}
	return &Result{Kind: "issue", URL: firstURL(out)}, nil
}

func createPR(ctx context.Context, repo, title, body, branch string) (*Result, error) {
	if strings.TrimSpace(branch) == "" {
		return nil, fmt.Errorf("开 PR 需要已推送的分支名")
	}
	out, err := gh(ctx, "pr", "create", "-R", repo, "-H", branch, "-t", title, "-b", body)
	if err != nil {
		return nil, err
	}
	return &Result{Kind: "pr", URL: firstURL(out)}, nil
}

func gh(ctx context.Context, args ...string) (string, error) {
	cmd := sysproc.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gh %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func firstURL(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			return line
		}
	}
	return s
}

// Resolved 查询已绑定的上游票据是否已结束。查询失败由调用方按“状态未知”处理。
func Resolved(ctx context.Context, url string) (bool, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return false, nil
	}
	kind := ""
	switch {
	case strings.Contains(url, "/pull/"):
		kind = "pr"
	case strings.Contains(url, "/issues/"):
		kind = "issue"
	default:
		return false, fmt.Errorf("无法识别上游 URL %q", url)
	}
	out, err := gh(ctx, kind, "view", url, "--json", "state")
	if err != nil {
		return false, err
	}
	var state struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		return false, err
	}
	return strings.EqualFold(state.State, "closed") || strings.EqualFold(state.State, "merged"), nil
}
