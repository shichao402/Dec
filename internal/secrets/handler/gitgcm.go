package handler

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/sysproc"
	"gopkg.in/yaml.v3"
)

// GCMDoc 是 .gcm/* 正文结构（gcm 处理器自有契约）。
type GCMDoc struct {
	Kind     string `yaml:"kind"`
	Host     string `yaml:"host"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Protocol string `yaml:"protocol,omitempty"`
	Provider string `yaml:"provider,omitempty"`
}

// GitGCMDoc 是 GCMDoc 的旧名别名（测试 / 迁移兼容）。
type GitGCMDoc = GCMDoc

// GCMIdentity 是 GCM Note 可安全展示/匹配的非秘密元数据。
// Password 永不进入该类型，避免候选列表或 RPC 响应泄露 token。
type GCMIdentity struct {
	Host     string
	Username string
	Protocol string
}

// GitRunner 执行 git 子命令；测试可注入。
type GitRunner func(ctx context.Context, stdin string, args ...string) error

// GCMHandler 将 Note 写入 Git Credential Manager（via git credential）。
type GCMHandler struct {
	Run GitRunner
}

// GitGCMHandler 是 GCMHandler 的旧名别名。
type GitGCMHandler = GCMHandler

// NewGCMHandler 使用给定 runner；nil 则用真实 git。
func NewGCMHandler(run GitRunner) *GCMHandler {
	if run == nil {
		run = defaultGitRunner
	}
	return &GCMHandler{Run: run}
}

// NewGitGCMHandler 是 NewGCMHandler 的旧名。
func NewGitGCMHandler(run GitRunner) *GCMHandler {
	return NewGCMHandler(run)
}

func (h *GCMHandler) Kind() string       { return "gcm" }
func (h *GCMHandler) Source() SourceKind { return SourceNote }

func (h *GCMHandler) Match(name string) bool {
	return MatchGCMPath(name)
}

// InspectGCM 解析 GCM Note 的非秘密身份信息，用于 repo bootstrap 按 host 查找候选。
func InspectGCM(name, content string) (GCMIdentity, error) {
	res, err := resolveGCM(Item{Name: name, NoteContent: content}, false)
	if err != nil {
		return GCMIdentity{}, err
	}
	return GCMIdentity{Host: res.host, Username: res.user, Protocol: res.protocol}, nil
}

func (h *GCMHandler) Apply(ctx context.Context, item Item) error {
	res, err := resolveGCM(item, true)
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

// Revoke 撤销 Apply 写入的 Git Credential Manager 凭据。
func (h *GCMHandler) Revoke(ctx context.Context, item Item) error {
	res, err := resolveGCM(item, false)
	if err != nil {
		return err
	}

	stdin := fmt.Sprintf("protocol=%s\nhost=%s\nusername=%s\n\n", res.protocol, res.host, res.user)
	if err := h.Run(ctx, stdin, "credential", "reject"); err != nil {
		return fmt.Errorf("git credential reject: %w", err)
	}

	if err := h.Run(ctx, "", "config", "--global", "--unset", res.credKey); err != nil {
		return nil
	}
	return nil
}

type gcmResolved struct {
	protocol string
	provider string
	host     string
	user     string
	pass     string
	credKey  string
}

func resolveGCM(item Item, requirePassword bool) (gcmResolved, error) {
	doc, err := parseGCMDoc(item.NoteContent)
	if err != nil {
		return gcmResolved{}, err
	}
	if !MatchGCMPath(item.Name) {
		return gcmResolved{}, fmt.Errorf("note 路径不符合 .gcm/*: %q", item.Name)
	}
	if k := strings.TrimSpace(doc.Kind); k != "" && k != "gcm" && k != "gitgcm" {
		return gcmResolved{}, fmt.Errorf("YAML kind=%q，期望 gcm（或留空）", doc.Kind)
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
		return gcmResolved{}, fmt.Errorf("gcm 需要非空 host、username")
	}
	if requirePassword && pass == "" {
		return gcmResolved{}, fmt.Errorf("gcm 需要非空 password")
	}
	if strings.ContainsAny(host, " \t\r\n") || strings.Contains(host, "://") {
		return gcmResolved{}, fmt.Errorf("非法 host: %q", host)
	}
	if protocol != "https" && protocol != "http" {
		return gcmResolved{}, fmt.Errorf("不支持的 protocol: %q", protocol)
	}

	return gcmResolved{
		protocol: protocol,
		provider: provider,
		host:     host,
		user:     user,
		pass:     pass,
		credKey:  fmt.Sprintf("credential.%s://%s.provider", protocol, host),
	}, nil
}

func parseGCMDoc(content string) (*GCMDoc, error) {
	var doc GCMDoc
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("解析 gcm YAML: %w", err)
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
