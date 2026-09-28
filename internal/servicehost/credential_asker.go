package servicehost

import (
	"context"
	"fmt"

	"github.com/shichao402/Dec/internal/consoleopen"
)

// brokerCredentialAsker 把 credentialBroker 适配成 app.CredentialAsker：
// 挂起请求 → 唤起 Console → 阻塞等待提交。秘密只在 broker 内存与调用方之间流转。
type brokerCredentialAsker struct {
	server *Server
}

// newBrokerCredentialAsker 构造适配器。server 为 nil 时返回 nil（headless 语义
// 由 app 层 credentialAsker==nil 分支处理）。
func newBrokerCredentialAsker(server *Server) *brokerCredentialAsker {
	if server == nil {
		return nil
	}
	return &brokerCredentialAsker{server: server}
}

// AskLoginPassword 挂起登录密码请求并等 Console 提交。username 留空时由
// app 层默认 root。
func (a *brokerCredentialAsker) AskLoginPassword(ctx context.Context, prompt, operation string) (string, string, error) {
	return a.ask(ctx, CredentialKindLoginPassword, prompt, operation)
}

// AskKeyPassphrase 挂起私钥口令请求并等 Console 提交。
func (a *brokerCredentialAsker) AskKeyPassphrase(ctx context.Context, prompt, operation string) (string, error) {
	_, secret, err := a.ask(ctx, CredentialKindKeyPassphrase, prompt, operation)
	return secret, err
}

func (a *brokerCredentialAsker) ask(ctx context.Context, kind CredentialRequestKind, prompt, operation string) (string, string, error) {
	if a == nil || a.server == nil || a.server.credentials == nil {
		return "", "", fmt.Errorf("凭据请求通道不可用")
	}
	// 唤起失败不立即失败：Console 可能已经打开（例如用户刚解锁过），
	// broker 挂起后若无人拉取将按 TTL 超时，错误信息可兜底。
	_ = consoleopen.OpenCredentialRequest()
	sub, err := a.server.credentials.Await(ctx, CredentialRequestSpec{
		Kind:      kind,
		Prompt:    prompt,
		Operation: operation,
	})
	if err != nil {
		return "", "", err
	}
	return "", sub.secret, nil
}
