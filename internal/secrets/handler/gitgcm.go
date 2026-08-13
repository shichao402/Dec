package handler

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

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
	doc, err := parseGitGCMDoc(item.NoteContent)
	if err != nil {
		return err
	}
	if _, processor, ok := ParseProcessorNoteName(item.Name); !ok || processor != "gitgcm" {
		return fmt.Errorf("note 名不符合 *_gitgcm.yaml: %q", item.Name)
	}
	if doc.Kind != "gitgcm" {
		return fmt.Errorf("YAML kind=%q，与文件名处理器 gitgcm 不一致", doc.Kind)
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
	if host == "" || user == "" || pass == "" {
		return fmt.Errorf("gitgcm 需要非空 host、username、password")
	}
	if strings.ContainsAny(host, " \t\r\n") || strings.Contains(host, "://") {
		return fmt.Errorf("非法 host: %q", host)
	}
	if protocol != "https" && protocol != "http" {
		return fmt.Errorf("不支持的 protocol: %q", protocol)
	}

	credKey := fmt.Sprintf("credential.%s://%s.provider", protocol, host)
	if err := h.Run(ctx, "", "config", "--global", credKey, provider); err != nil {
		return fmt.Errorf("设置 %s: %w", credKey, err)
	}

	stdin := fmt.Sprintf("protocol=%s\nhost=%s\nusername=%s\npassword=%s\n\n", protocol, host, user, pass)
	if err := h.Run(ctx, stdin, "credential", "approve"); err != nil {
		return fmt.Errorf("git credential approve: %w", err)
	}
	return nil
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
	cmd := exec.CommandContext(ctx, "git", args...)
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
