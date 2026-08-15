package handler

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/sysproc"
	"gopkg.in/yaml.v3"
)

// GitGCMDoc 是 *_gitgcm.yaml 的正文结构。
type GitGCMDoc struct {
	Kind     string `yaml:"kind"`
	Host     string `yaml:"host"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Protocol string `yaml:"protocol,omitempty"`
	Provider string `yaml:"provider,omitempty"`
}

// GitRunner 执行 git 子命令；测试可注入。
type GitRunner func(ctx context.Context, stdin string, args ...string) error

// GitGCMHandler 将 YAML Note 写入 Git Credential Manager（via git credential）。
type GitGCMHandler struct {
	Run GitRunner
}

// NewGitGCMHandler 使用给定 runner；nil 则用真实 git。
func NewGitGCMHandler(run GitRunner) *GitGCMHandler {
	if run == nil {
		run = defaultGitRunner
	}
	return &GitGCMHandler{Run: run}
}

func (h *GitGCMHandler) Kind() string       { return "gitgcm" }
func (h *GitGCMHandler) Source() SourceKind { return SourceNote }

func (h *GitGCMHandler) Match(name string) bool {
	_, processor, ok := ParseProcessorNoteName(name)
	return ok && processor == "gitgcm"
}

func (h *GitGCMHandler) Apply(ctx context.Context, item Item) error {
	res, err := resolveGitGCM(item, true)
	if err != nil {
		return err
	}

	if err := h.Run(ctx, "", "config", "--global", res.credKey, res.provider); err != nil {
		return fmt.Errorf("设置 %s: %w", res.credKey, err)
	}

	stdin := fmt.Sprintf("protocol=%s\nhost=%s\nusername=%s\npassword=%s\n\n", res.protocol, res.host, res.user, res.pass)
	if err := h.Run(ctx, stdin, "credential", "approve"); err != nil {
		return fmt.Errorf("git credential approve: %w", err)
	}
	return nil
}

// Revoke 撤销 Apply 写入的 Git Credential Manager 凭据：
//   - git credential reject（同 protocol/host/username；password 可省略）
//   - git config --global --unset credential.<proto>://<host>.provider（不存在时容忍）
func (h *GitGCMHandler) Revoke(ctx context.Context, item Item) error {
	res, err := resolveGitGCM(item, false)
	if err != nil {
		return err
	}

	stdin := fmt.Sprintf("protocol=%s\nhost=%s\nusername=%s\n\n", res.protocol, res.host, res.user)
	if err := h.Run(ctx, stdin, "credential", "reject"); err != nil {
		return fmt.Errorf("git credential reject: %w", err)
	}

	// key 不存在时 git 返回退出码 5；撤销语义下视为已满足，容忍失败。
	if err := h.Run(ctx, "", "config", "--global", "--unset", res.credKey); err != nil {
		return nil
	}
	return nil
}

// gitGCMResolved 是从 Item 解析并校验后的 gitgcm 参数。
type gitGCMResolved struct {
	protocol string
	provider string
	host     string
	user     string
	pass     string
	credKey  string
}

func resolveGitGCM(item Item, requirePassword bool) (gitGCMResolved, error) {
	doc, err := parseGitGCMDoc(item.NoteContent)
	if err != nil {
		return gitGCMResolved{}, err
	}
	if _, processor, ok := ParseProcessorNoteName(item.Name); !ok || processor != "gitgcm" {
		return gitGCMResolved{}, fmt.Errorf("note 名不符合 *_gitgcm.yaml: %q", item.Name)
	}
	if doc.Kind != "gitgcm" {
		return gitGCMResolved{}, fmt.Errorf("YAML kind=%q，与文件名处理器 gitgcm 不一致", doc.Kind)
	}

	protocol := doc.Protocol
	if protocol == "" {
		protocol = "https"
	}
	provider := doc.Provider
	if provider == "" {
		provider = "generic"
	}
	host := strings.TrimSpace(doc.Host)
	user := strings.TrimSpace(doc.Username)
	pass := doc.Password
	if host == "" || user == "" {
		return gitGCMResolved{}, fmt.Errorf("gitgcm 需要非空 host、username")
	}
	if requirePassword && pass == "" {
		return gitGCMResolved{}, fmt.Errorf("gitgcm 需要非空 password")
	}
	if strings.ContainsAny(host, " \t\r\n") || strings.Contains(host, "://") {
		return gitGCMResolved{}, fmt.Errorf("非法 host: %q", host)
	}
	if protocol != "https" && protocol != "http" {
		return gitGCMResolved{}, fmt.Errorf("不支持的 protocol: %q", protocol)
	}

	return gitGCMResolved{
		protocol: protocol,
		provider: provider,
		host:     host,
		user:     user,
		pass:     pass,
		credKey:  fmt.Sprintf("credential.%s://%s.provider", protocol, host),
	}, nil
}

func parseGitGCMDoc(content string) (*GitGCMDoc, error) {
	var doc GitGCMDoc
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("解析 gitgcm YAML: %w", err)
	}
	doc.Kind = strings.TrimSpace(doc.Kind)
	doc.Host = strings.TrimSpace(doc.Host)
	doc.Username = strings.TrimSpace(doc.Username)
	doc.Protocol = strings.TrimSpace(strings.ToLower(doc.Protocol))
	doc.Provider = strings.TrimSpace(doc.Provider)
	return &doc, nil
}

func defaultGitRunner(ctx context.Context, stdin string, args ...string) error {
	cmd := sysproc.CommandContext(ctx, "git", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}
