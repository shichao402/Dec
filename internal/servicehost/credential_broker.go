package servicehost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
)

// credentialBroker 是 ADR 0036 凭据请求通道的服务端内存态（阶段 B）。
//
// Agent 工具在操作中缺凭据时调用 Await：挂起一个请求、唤起 Console、阻塞等待
// 用户经 SubmitCredential 提交。Console 用 PullCredentialRequest 拉取描述渲染
// 控件。秘密值只在 broker 的 pending 结构与等待方返回值之间流转，不落盘、
// 不进日志。同一时刻只允许一个挂起请求：凭据交互是用户专注事件，多请求并行
// 会让 Console 的单请求 UI 语义破碎，后来者直接报错。
type credentialBroker struct {
	mu      sync.Mutex
	pending *pendingCredentialRequest
}

type pendingCredentialRequest struct {
	id         string
	kind       CredentialRequestKind
	prompt     string
	operation  string
	createdAt  time.Time
	expiresAt  time.Time
	submitted  chan credentialSubmission
	finished   bool
}

// credentialSubmission 是一次用户提交（或取消）的结果快照。
type credentialSubmission struct {
	secret    string
	publicKey string
	approved  bool
	canceled  bool
}

const (
	defaultCredentialRequestTTL   = 5 * time.Minute
	credentialRequestIDPrefix     = "cred-"
)

// ErrCredentialRequestPending 已有挂起请求，拒绝并发第二个。
var ErrCredentialRequestPending = errors.New("已有一个凭据请求在等待用户，请先在 Console 中处理")

var (
	ErrCredentialRequestNotFound = errors.New("凭据请求不存在或已结束")
	ErrCredentialRequestExpired  = errors.New("凭据请求已超时")
)

func newCredentialBroker() *credentialBroker {
	return &credentialBroker{}
}

// CredentialRequestSpec 描述一次挂起请求（Await 的输入）。
type CredentialRequestSpec struct {
	Kind      CredentialRequestKind
	Prompt    string
	Operation string
	TTL       time.Duration
}

// Await 挂起请求并阻塞等待用户提交。调用方应先自行唤起 Console（或由本函数
// 之外的重试逻辑处理唤起失败）；ctx 取消时请求自动清理。
func (b *credentialBroker) Await(ctx context.Context, spec CredentialRequestSpec) (credentialSubmission, error) {
	if spec.TTL <= 0 {
		spec.TTL = defaultCredentialRequestTTL
	}
	id, err := newCredentialRequestID()
	if err != nil {
		return credentialSubmission{}, fmt.Errorf("生成凭据请求 ID 失败: %w", err)
	}
	req := &pendingCredentialRequest{
		id:        id,
		kind:      spec.Kind,
		prompt:    spec.Prompt,
		operation: spec.Operation,
		createdAt: time.Now(),
		expiresAt: time.Now().Add(spec.TTL),
		submitted: make(chan credentialSubmission, 1),
	}

	b.mu.Lock()
	if b.pending != nil {
		b.mu.Unlock()
		return credentialSubmission{}, ErrCredentialRequestPending
	}
	b.pending = req
	b.mu.Unlock()
	defer b.release(id)

	select {
	case sub := <-req.submitted:
		if sub.canceled {
			return sub, ErrCredentialRequestCanceled
		}
		return sub, nil
	case <-time.After(time.Until(req.expiresAt)):
		return credentialSubmission{}, ErrCredentialRequestExpired
	case <-ctx.Done():
		return credentialSubmission{}, ctx.Err()
	}
}

// release 清理挂起请求（幂等：仅当 id 匹配且未清理时）。
func (b *credentialBroker) release(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending != nil && b.pending.id == id {
		b.pending = nil
	}
}

// Pull 返回当前挂起请求的公开描述（不含任何秘密）。
func (b *credentialBroker) Pull() (requestID string, kind CredentialRequestKind, prompt, operation string, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil {
		return "", 0, "", "", false
	}
	p := b.pending
	if time.Now().After(p.expiresAt) {
		return "", 0, "", "", false
	}
	return p.id, p.kind, p.prompt, p.operation, true
}

// Submit 提交用户输入。id 不匹配、请求已结束或已过期时返回错误。
func (b *credentialBroker) Submit(id string, sub credentialSubmission) error {
	b.mu.Lock()
	p := b.pending
	if p == nil || p.id != id || p.finished {
		b.mu.Unlock()
		return ErrCredentialRequestNotFound
	}
	if time.Now().After(p.expiresAt) {
		p.finished = true
		b.pending = nil
		b.mu.Unlock()
		return ErrCredentialRequestExpired
	}
	p.finished = true
	b.pending = nil
	b.mu.Unlock()
	select {
	case p.submitted <- sub:
	default:
		// 等待方已离开（超时/取消），结果丢弃。
	}
	return nil
}

// HasPending 报告是否有挂起请求（诊断与测试用）。
func (b *credentialBroker) HasPending() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending != nil
}

func newCredentialRequestID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return credentialRequestIDPrefix + hex.EncodeToString(buf), nil
}

// CredentialRequestKind 与 proto 枚举同名的本地别名，避免 servicehost 包对
// schema 生成代码的散点引用。
type CredentialRequestKind = servicev1.CredentialRequestKind

// 常用 kind 集合（阶段 C 工具面使用）。
var (
	CredentialKindLoginPassword    = servicev1.CredentialRequestKind_CREDENTIAL_REQUEST_KIND_LOGIN_PASSWORD
	CredentialKindKeyPassphrase    = servicev1.CredentialRequestKind_CREDENTIAL_REQUEST_KIND_KEY_PASSPHRASE
	CredentialKindPrivateKeyMaterial = servicev1.CredentialRequestKind_CREDENTIAL_REQUEST_KIND_PRIVATE_KEY_MATERIAL
	CredentialKindConfirm          = servicev1.CredentialRequestKind_CREDENTIAL_REQUEST_KIND_CONFIRM
)

var ErrCredentialRequestCanceled = errors.New("用户在 Console 中取消了凭据请求")
