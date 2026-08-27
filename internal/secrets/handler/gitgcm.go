package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/shichao402/Dec/internal/secrets"
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
	Path     string `yaml:"path,omitempty"`
}

// GitGCMDoc 是 GCMDoc 的旧名别名（测试 / 迁移兼容）。
type GitGCMDoc = GCMDoc

// GCMIdentity 是 GCM Note 可安全展示/匹配的非秘密元数据。
// Password 永不进入该类型，避免候选列表或 RPC 响应泄露 token。
type GCMIdentity struct {
	Host     string
	Username string
	Protocol string
	Path     string
}

// GitRunner 执行 git 子命令；测试可注入。
type GitRunner func(ctx context.Context, stdin string, args ...string) error
type GitOutputRunner func(ctx context.Context, args ...string) (string, error)

// GCMHandler 将 Note 写入 Git Credential Manager（via git credential）。
type GCMHandler struct {
	Run    GitRunner
	Output GitOutputRunner
}

// GitGCMHandler 是 GCMHandler 的旧名别名。
type GitGCMHandler = GCMHandler

var gcmProviderRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// NewGCMHandler 使用给定 runner；nil 则用真实 git。
func NewGCMHandler(run GitRunner) *GCMHandler {
	if run == nil {
		run = defaultGitRunner
	}
	return &GCMHandler{Run: run, Output: defaultGitOutputRunner}
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
	return GCMIdentity{Host: res.host, Username: res.user, Protocol: res.protocol, Path: res.path}, nil
}

func (h *GCMHandler) Apply(ctx context.Context, item Item) error {
	res, err := resolveGCM(item, true)
	if err != nil {
		return err
	}
	if item.ProjectScoped {
		if strings.TrimSpace(item.ProjectRoot) == "" {
			return fmt.Errorf("project-scoped GCM 缺少 projectRoot")
		}
		return h.applyProject(ctx, item, res)
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
	if item.ProjectScoped {
		if strings.TrimSpace(item.ProjectRoot) == "" {
			return fmt.Errorf("project-scoped GCM 缺少 projectRoot")
		}
		return h.revokeProject(ctx, item, res)
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
	path     string
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
	if strings.ContainsAny(user, "\r\n") || strings.ContainsAny(pass, "\r\n") {
		return gcmResolved{}, fmt.Errorf("gcm username/password 不能包含换行")
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
	if !gcmProviderRe.MatchString(provider) {
		return gcmResolved{}, fmt.Errorf("非法 GCM provider: %q", provider)
	}
	docPath := strings.Trim(strings.TrimSpace(doc.Path), "/")
	if strings.ContainsAny(docPath, "\r\n") {
		return gcmResolved{}, fmt.Errorf("GCM path 不能包含换行")
	}

	return gcmResolved{
		protocol: protocol,
		provider: provider,
		host:     host,
		user:     user,
		pass:     pass,
		credKey:  fmt.Sprintf("credential.%s://%s.provider", protocol, host),
		path:     docPath,
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
	doc.Path = strings.Trim(strings.TrimSpace(strings.ReplaceAll(doc.Path, "\\", "/")), "/")
	return &doc, nil
}

func (h *GCMHandler) applyProject(ctx context.Context, item Item, res gcmResolved) error {
	resolved, err := h.resolveProjectCredential(ctx, item.ProjectRoot, res)
	if err != nil {
		return err
	}
	if err := upsertProjectGCMBlock(item.ProjectRoot, resolved); err != nil {
		return err
	}
	stdin := projectCredentialInput(resolved, true)
	if err := h.Run(ctx, stdin, "-C", item.ProjectRoot, "credential", "approve"); err != nil {
		_ = removeProjectGCMBlock(item.ProjectRoot, resolved)
		return fmt.Errorf("project git credential approve: %w", err)
	}
	return nil
}

func (h *GCMHandler) revokeProject(ctx context.Context, item Item, res gcmResolved) error {
	resolved, err := h.resolveProjectCredential(ctx, item.ProjectRoot, res)
	if err != nil {
		return err
	}
	stdin := projectCredentialInput(resolved, false)
	if err := h.Run(ctx, stdin, "-C", item.ProjectRoot, "credential", "reject"); err != nil {
		return fmt.Errorf("project git credential reject: %w", err)
	}
	return removeProjectGCMBlock(item.ProjectRoot, resolved)
}

func (h *GCMHandler) resolveProjectCredential(ctx context.Context, projectRoot string, res gcmResolved) (gcmResolved, error) {
	if res.path != "" {
		return res, nil
	}
	output := h.Output
	if output == nil {
		output = defaultGitOutputRunner
	}
	remoteURL, err := output(ctx, "-C", projectRoot, "remote", "get-url", "origin")
	if err != nil {
		return gcmResolved{}, fmt.Errorf("project GCM 需要 path 或可读取的 origin URL: %w", err)
	}
	protocol, host, repoPath, err := parseCredentialRemoteURL(remoteURL)
	if err != nil {
		return gcmResolved{}, err
	}
	if !strings.EqualFold(protocol, res.protocol) || !strings.EqualFold(host, res.host) {
		return gcmResolved{}, fmt.Errorf("GCM Note %s://%s 与工作区 origin %s://%s 不匹配", res.protocol, res.host, protocol, host)
	}
	res.path = repoPath
	return res, nil
}

func parseCredentialRemoteURL(raw string) (protocol, host, repoPath string, err error) {
	raw = strings.TrimSpace(raw)
	u, parseErr := url.Parse(raw)
	if parseErr != nil || u.Scheme == "" || u.Hostname() == "" {
		return "", "", "", fmt.Errorf("project GCM 仅支持 HTTP(S) origin，收到 %q", raw)
	}
	protocol = strings.ToLower(u.Scheme)
	if protocol != "http" && protocol != "https" {
		return "", "", "", fmt.Errorf("project GCM 仅支持 HTTP(S) origin，收到 %q", raw)
	}
	host = u.Host
	repoPath = strings.Trim(strings.TrimSuffix(path.Clean(u.EscapedPath()), ".git"), "/")
	if repoPath == "" || repoPath == "." {
		return "", "", "", fmt.Errorf("origin URL 缺少仓库路径: %q", raw)
	}
	return protocol, host, repoPath, nil
}

func projectCredentialInput(res gcmResolved, withPassword bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "protocol=%s\nhost=%s\npath=%s\nusername=%s\n", res.protocol, res.host, res.path, res.user)
	if withPassword {
		fmt.Fprintf(&b, "password=%s\n", res.pass)
	}
	b.WriteByte('\n')
	return b.String()
}

func projectGCMMarkers(res gcmResolved) (string, string) {
	sum := sha256.Sum256([]byte(strings.ToLower(res.protocol + "\x00" + res.host + "\x00" + res.path)))
	suffix := fmt.Sprintf(" %x", sum[:6])
	return "# BEGIN DEC PROJECT GCM" + suffix, "# END DEC PROJECT GCM" + suffix
}

func upsertProjectGCMBlock(projectRoot string, res gcmResolved) error {
	begin, end := projectGCMMarkers(res)
	body := fmt.Sprintf("[credential]\n\tuseHttpPath = true\n[credential %q]\n\tprovider = %s",
		res.protocol+"://"+res.host+"/"+res.path, res.provider)
	return secrets.UpsertProjectGitBlock(projectRoot, begin, end, body)
}

func removeProjectGCMBlock(projectRoot string, res gcmResolved) error {
	begin, end := projectGCMMarkers(res)
	return secrets.UpsertProjectGitBlock(projectRoot, begin, end, "")
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

func defaultGitOutputRunner(ctx context.Context, args ...string) (string, error) {
	cmd := sysproc.CommandContext(ctx, "git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
