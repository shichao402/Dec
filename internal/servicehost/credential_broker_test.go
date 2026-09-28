package servicehost

import (
	"context"
	"errors"
	"testing"
	"time"

	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
)

func TestCredentialBrokerAwaitSubmit(t *testing.T) {
	b := newCredentialBroker()
	go func() {
		waitForPending(t, b)
		if err := b.Submit("cred-nope", credentialSubmission{secret: "x"}); !errors.Is(err, ErrCredentialRequestNotFound) {
			t.Errorf("错误 id 提交应失败: %v", err)
		}
		id, _, _, _, ok := b.Pull()
		if !ok {
			t.Fatal("应有挂起请求")
		}
		if err := b.Submit(id, credentialSubmission{secret: "hunter2"}); err != nil {
			t.Errorf("提交: %v", err)
		}
	}()
	sub, err := b.Await(context.Background(), CredentialRequestSpec{
		Kind:      CredentialKindLoginPassword,
		Prompt:    "为设备 dev-box 提供登录密码",
		Operation: "dec_install_ssh_key",
		TTL:       5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.secret != "hunter2" {
		t.Fatalf("secret = %q", sub.secret)
	}
	if b.HasPending() {
		t.Fatal("提交后不应残留挂起请求")
	}
}

func TestCredentialBrokerCancel(t *testing.T) {
	b := newCredentialBroker()
	go func() {
		waitForPending(t, b)
		id, kind, prompt, op, ok := b.Pull()
		if !ok {
			t.Fatal("应有挂起请求")
		}
		if kind != CredentialKindConfirm || prompt == "" || op != "dec_rotate_ssh_key" {
			t.Fatalf("pull = %q %q %q", kind, prompt, op)
		}
		if err := b.Submit(id, credentialSubmission{canceled: true}); err != nil {
			t.Errorf("取消提交: %v", err)
		}
	}()
	_, err := b.Await(context.Background(), CredentialRequestSpec{
		Kind:      CredentialKindConfirm,
		Prompt:    "确认在远端 authorized_keys 更换密钥",
		Operation: "dec_rotate_ssh_key",
		TTL:       5 * time.Second,
	})
	if !errors.Is(err, ErrCredentialRequestCanceled) {
		t.Fatalf("取消应返回 ErrCredentialRequestCanceled: %v", err)
	}
}

// waitForPending 轮询等待挂起请求出现，消除 goroutine 与 Await 之间的注册竞态。
func waitForPending(t *testing.T, b *credentialBroker) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !b.HasPending() {
		if time.Now().After(deadline) {
			t.Fatal("等待挂起超时")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCredentialBrokerTimeout(t *testing.T) {
	b := newCredentialBroker()
	_, err := b.Await(context.Background(), CredentialRequestSpec{
		Kind: CredentialKindLoginPassword,
		TTL:  50 * time.Millisecond,
	})
	if !errors.Is(err, ErrCredentialRequestExpired) {
		t.Fatalf("超时应返回 ErrCredentialRequestExpired: %v", err)
	}
	if b.HasPending() {
		t.Fatal("超时后不应残留挂起请求")
	}
}

func TestCredentialBrokerCtxCancel(t *testing.T) {
	b := newCredentialBroker()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := b.Await(ctx, CredentialRequestSpec{Kind: CredentialKindKeyPassphrase})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ctx 取消应透传: %v", err)
	}
	if b.HasPending() {
		t.Fatal("ctx 取消后不应残留挂起请求")
	}
}

func TestCredentialBrokerRejectsConcurrent(t *testing.T) {
	b := newCredentialBroker()
	first := make(chan struct{})
	go func() {
		_, err := b.Await(context.Background(), CredentialRequestSpec{
			Kind: CredentialKindLoginPassword,
			TTL:  200 * time.Millisecond,
		})
		if !errors.Is(err, ErrCredentialRequestExpired) {
			t.Errorf("第一个请求应等到超时: %v", err)
		}
		close(first)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !b.HasPending() {
		if time.Now().After(deadline) {
			t.Fatal("等待挂起超时")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, err := b.Await(context.Background(), CredentialRequestSpec{Kind: CredentialKindLoginPassword})
	if !errors.Is(err, ErrCredentialRequestPending) {
		t.Fatalf("并发第二请求应被拒: %v", err)
	}
	<-first
}

func TestCredentialBrokerPullHidesSecret(t *testing.T) {
	b := newCredentialBroker()
	go b.Submit("cred-x", credentialSubmission{}) // 无效提交，不影响挂起
	resultCh := make(chan credentialSubmission, 1)
	go func() {
		sub, err := b.Await(context.Background(), CredentialRequestSpec{
			Kind:      CredentialKindPrivateKeyMaterial,
			Prompt:    "粘贴存量私钥",
			Operation: "dec_import_sshkey",
			TTL:       100 * time.Millisecond,
		})
		if err != nil {
			t.Errorf("await: %v", err)
		}
		resultCh <- sub
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !b.HasPending() {
		if time.Now().After(deadline) {
			t.Fatal("等待挂起超时")
		}
		time.Sleep(5 * time.Millisecond)
	}
	id, kind, prompt, op, ok := b.Pull()
	if !ok {
		t.Fatal("pull 失败")
	}
	if id == "" || kind != CredentialKindPrivateKeyMaterial || prompt != "粘贴存量私钥" || op != "dec_import_sshkey" {
		t.Fatalf("pull = %q %q %q %q", id, kind, prompt, op)
	}
	if err := b.Submit(id, credentialSubmission{secret: "PRIVATE", publicKey: "PUB"}); err != nil {
		t.Fatal(err)
	}
	sub := <-resultCh
	if sub.secret != "PRIVATE" || sub.publicKey != "PUB" {
		t.Fatalf("submission = %+v", sub)
	}
}

func TestCredentialRequestRPCAllowedWhenLocked(t *testing.T) {
	// 锁定态预授权：凭据拉取与提交必须在 methodAllowedWhenLocked 白名单内，
	// 否则 Console 被唤起时（服务可能未解锁）无法完成凭据交互。
	for _, name := range []string{
		servicev1.DecService_PullCredentialRequest_FullMethodName,
		servicev1.DecService_SubmitCredential_FullMethodName,
	} {
		if !methodAllowedWhenLocked(name) {
			t.Fatalf("%s 应允许在锁定态调用", name)
		}
	}
}
