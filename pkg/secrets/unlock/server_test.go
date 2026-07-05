package unlock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestServerUnlock_WritesSession(t *testing.T) {
	t.Parallel()

	var session atomic.Value
	auth := NewStubAuthenticator("secret", "", "token-abc")
	srv := newServer(auth, func(token string) {
		session.Store(token)
	})
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.PostForm(ts.URL+"/unlock", url.Values{"password": {"secret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "解锁成功") {
		t.Fatalf("期望成功页: %s", body)
	}
	if got := session.Load(); got != "token-abc" {
		t.Fatalf("session = %v, 期望 token-abc", got)
	}
}

func TestServerUnlock_RejectsWrongPassword(t *testing.T) {
	t.Parallel()

	auth := NewStubAuthenticator("secret", "", "token-abc")
	srv := newServer(auth, func(string) { t.Fatal("不应写入 session") })
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.PostForm(ts.URL+"/unlock", url.Values{"password": {"wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "主密码不正确") {
		t.Fatalf("期望密码错误提示: %s", body)
	}
}

func TestServerUnlock_2FAFlow(t *testing.T) {
	t.Parallel()

	var session atomic.Value
	auth := NewStubAuthenticator("secret", "654321", "token-2fa")
	srv := newServer(auth, func(token string) {
		session.Store(token)
	})
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp1, err := client.PostForm(ts.URL+"/unlock", url.Values{"password": {"secret"}})
	if err != nil {
		t.Fatal(err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusSeeOther {
		t.Fatalf("第一步 status = %d, 期望 303", resp1.StatusCode)
	}

	resp2, err := client.Get(ts.URL + "/unlock/2fa")
	if err != nil {
		t.Fatal(err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if !strings.Contains(string(body2), "二次验证") {
		t.Fatalf("期望 2FA 页: %s", body2)
	}

	resp3, err := client.PostForm(ts.URL+"/unlock/2fa", url.Values{"code": {"654321"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	body3, _ := io.ReadAll(resp3.Body)
	if !strings.Contains(string(body3), "解锁成功") {
		t.Fatalf("期望成功页: %s", body3)
	}
	if got := session.Load(); got != "token-2fa" {
		t.Fatalf("session = %v, 期望 token-2fa", got)
	}
}

func TestRun_EndToEndWithMockBrowser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var openedURL string
	var storedSession string
	err := Run(ctx, Options{
		Authenticator: NewStubAuthenticator("pw", "", "sess-end"),
		OpenBrowser: func(openURL string) error {
			openedURL = openURL
			resp, postErr := http.PostForm(openURL, url.Values{"password": {"pw"}})
			if postErr != nil {
				return postErr
			}
			resp.Body.Close()
			return nil
		},
		OnSession: func(session string) {
			storedSession = session
		},
	})
	if err != nil {
		t.Fatalf("Run() 失败: %v", err)
	}
	if openedURL == "" {
		t.Fatal("浏览器未被打开")
	}
	if storedSession != "sess-end" {
		t.Fatalf("session = %q, 期望 sess-end", storedSession)
	}
}

func TestRun_BrowserOpenFailureContinues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var readyURL string
	var storedSession string
	err := Run(ctx, Options{
		Authenticator: NewStubAuthenticator("pw", "", "sess-manual"),
		OpenBrowser: func(string) error {
			return fmt.Errorf("mock browser failure")
		},
		OnReady: func(unlockURL string) {
			readyURL = unlockURL
			resp, postErr := http.PostForm(unlockURL, url.Values{"password": {"pw"}})
			if postErr != nil {
				t.Errorf("manual unlock POST: %v", postErr)
				return
			}
			resp.Body.Close()
		},
		OnSession: func(session string) {
			storedSession = session
		},
	})
	if err != nil {
		t.Fatalf("Run() 失败: %v", err)
	}
	if readyURL == "" {
		t.Fatal("OnReady 未被调用")
	}
	if storedSession != "sess-manual" {
		t.Fatalf("session = %q, 期望 sess-manual", storedSession)
	}
}

func TestRun_OnReadyBeforeBrowser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	order := make([]string, 0, 2)
	err := Run(ctx, Options{
		Authenticator: NewStubAuthenticator("pw", "", "sess-order"),
		OnReady: func(string) {
			order = append(order, "ready")
		},
		OpenBrowser: func(openURL string) error {
			order = append(order, "browser")
			resp, postErr := http.PostForm(openURL, url.Values{"password": {"pw"}})
			if postErr != nil {
				return postErr
			}
			resp.Body.Close()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run() 失败: %v", err)
	}
	if len(order) != 2 || order[0] != "ready" || order[1] != "browser" {
		t.Fatalf("回调顺序 = %v, 期望 [ready browser]", order)
	}
}

func TestRun_CancelContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, Options{
		Authenticator: NewStubAuthenticator("pw", "", "sess-end"),
	})
	if err == nil {
		t.Fatal("Run() 应返回取消错误")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %v, 期望 context.Canceled", err)
	}
}
