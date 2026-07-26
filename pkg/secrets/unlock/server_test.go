package unlock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func testNewServer(auth Authenticator, initialEmail string, onUnlock func(string), onEmailSaved func(string) error) *server {
	return newServer(auth, initialEmail, onUnlock, onEmailSaved)
}

func inputTag(html, id string) string {
	needle := `id="` + id + `"`
	idx := strings.Index(html, needle)
	if idx < 0 {
		return ""
	}
	start := strings.LastIndex(html[:idx], "<input")
	if start < 0 {
		return ""
	}
	end := strings.Index(html[start:], ">")
	if end < 0 {
		return ""
	}
	return html[start : start+end+1]
}

func assertVisibleInputsShareClass(t *testing.T, html, class string, ids ...string) {
	t.Helper()
	for _, id := range ids {
		tag := inputTag(html, id)
		if tag == "" {
			t.Fatalf("未找到 id=%q 的 input", id)
		}
		if !strings.Contains(tag, `class="`+class+`"`) {
			t.Fatalf("id=%q 的 input 缺少 class=%q: %s", id, class, tag)
		}
		if strings.Contains(tag, "decoy-field") {
			t.Fatalf("id=%q 的可见 input 不应使用 decoy-field: %s", id, tag)
		}
	}
}

func TestListenTCP_DefaultFixedPort(t *testing.T) {
	ln, err := listenTCP(resolveListenAddrs("")...)
	if err != nil {
		t.Fatalf("listenTCP() 失败: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	if port != fmt.Sprintf("%d", DefaultUnlockPort) {
		t.Fatalf("port = %s, 期望 %d", port, DefaultUnlockPort)
	}
}

func TestListenTCP_FallbackWhenFixedPortInUse(t *testing.T) {
	blocker, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", DefaultUnlockPort))
	if err != nil {
		t.Fatalf("占用固定端口失败: %v", err)
	}
	t.Cleanup(func() { _ = blocker.Close() })

	ln, err := listenTCP(resolveListenAddrs("")...)
	if err != nil {
		t.Fatalf("listenTCP() fallback 失败: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	if port == fmt.Sprintf("%d", DefaultUnlockPort) {
		t.Fatalf("固定端口占用后仍绑定 %d", DefaultUnlockPort)
	}
	if port == "0" {
		t.Fatalf("fallback 端口不应为 0: %s", ln.Addr())
	}
}

func TestRun_UsesDefaultFixedPort(t *testing.T) {
	ctx := context.Background()
	var readyURL string
	err := Run(ctx, Options{
		Authenticator: NewStubAuthenticator("pw", "", "sess-port"),
		OpenBrowser: func(openURL string) error {
			resp, postErr := http.PostForm(openURL, url.Values{
				"email":    {"user@example.com"},
				"password": {"pw"},
			})
			if postErr != nil {
				return postErr
			}
			resp.Body.Close()
			return nil
		},
		OnReady: func(unlockURL string) {
			readyURL = unlockURL
		},
	})
	if err != nil {
		t.Fatalf("Run() 失败: %v", err)
	}
	wantPort := fmt.Sprintf(":%d/", DefaultUnlockPort)
	if !strings.Contains(readyURL, wantPort) {
		t.Fatalf("OnReady URL = %q, 期望包含 %q", readyURL, wantPort)
	}
}

func TestUnlockPage_NoInitialEmail(t *testing.T) {
	t.Parallel()

	srv := testNewServer(NewStubAuthenticator("secret", "", "token"), "", func(string) {}, nil)
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/unlock")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	for _, want := range []string{
		`id="email"`,
		`name="email"`,
		`type="email"`,
		`id="password"`,
		`autofocus`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("无初始 email 时 unlock 页缺少 %q: %s", want, html)
		}
	}
	if strings.Contains(html, `value="`) {
		t.Fatalf("无初始 email 时不应预填 value: %s", html)
	}
	assertVisibleInputsShareClass(t, html, "input", "email", "password")
	if !strings.Contains(html, ".field input {") {
		t.Fatalf("unlock 页缺少统一 .field input 样式")
	}
}

func TestUnlockPage_PrefillsInitialEmail(t *testing.T) {
	t.Parallel()

	srv := testNewServer(NewStubAuthenticator("secret", "", "token"), "saved@example.com", func(string) {}, nil)
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/unlock")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, `value="saved@example.com"`) {
		t.Fatalf("期望预填 email: %s", html)
	}
}

func TestUnlockPage_SubmitUX(t *testing.T) {
	t.Parallel()

	auth := NewStubAuthenticator("secret", "", "token-abc")
	srv := testNewServer(auth, "", func(string) {}, nil)
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/unlock")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	for _, want := range []string{
		`id="unlock-form"`,
		`type="submit"`,
		"正在解锁…",
		"requestSubmit",
		`e.key === 'Enter'`,
		"aria-busy",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("unlock 页缺少 %q: %s", want, html)
		}
	}
}

func Test2FAPage_RedirectNoSpuriousError(t *testing.T) {
	t.Parallel()

	auth := NewStubAuthenticator("secret", "654321", "token-2fa")
	srv := testNewServer(auth, "", func(string) {}, nil)
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp1, err := client.PostForm(ts.URL+"/unlock", url.Values{
		"email":    {"user@example.com"},
		"password": {"secret"},
	})
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
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	html := string(body)
	if !strings.Contains(html, "二次验证") {
		t.Fatalf("期望 2FA 页: %s", html)
	}
	if strings.Contains(html, "当前不需要二次验证") {
		t.Fatalf("2FA 页不应显示 spurious 错误: %s", html)
	}
	if strings.Contains(html, `class="error"`) {
		t.Fatalf("2FA 页 GET 不应带错误: %s", html)
	}
}

func Test2FAPage_SubmitUX(t *testing.T) {
	t.Parallel()

	auth := NewStubAuthenticator("secret", "654321", "token-2fa")
	srv := testNewServer(auth, "", func(string) {}, nil)
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp1, err := client.PostForm(ts.URL+"/unlock", url.Values{
		"email":    {"user@example.com"},
		"password": {"secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp1.Body.Close()

	resp2, err := client.Get(ts.URL + "/unlock/2fa")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	html := string(body)
	for _, want := range []string{
		`id="2fa-form"`,
		`name="remember"`,
		"记住此设备",
		"正在验证…",
		"requestSubmit",
		`e.key === 'Enter'`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("2FA 页缺少 %q: %s", want, html)
		}
	}
	assertVisibleInputsShareClass(t, html, "input", "code")
}

func TestServerUnlock_WritesSession(t *testing.T) {
	t.Parallel()

	var session atomic.Value
	auth := NewStubAuthenticator("secret", "", "token-abc")
	srv := testNewServer(auth, "", func(token string) {
		session.Store(token)
	}, nil)
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.PostForm(ts.URL+"/unlock", url.Values{
		"email":    {"user@example.com"},
		"password": {"secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	html := string(body)
	if !strings.Contains(html, "解锁成功") {
		t.Fatalf("期望成功页: %s", html)
	}
	for _, want := range []string{
		"秒后自动关闭",
		`id="seconds"`,
		"window.close",
		"可手动关闭此标签页",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("成功页缺少 %q: %s", want, html)
		}
	}
	if got := session.Load(); got != "token-abc" {
		t.Fatalf("session = %v, 期望 token-abc", got)
	}
}

func TestServerUnlock_RejectsWrongPasswordRetainsEmail(t *testing.T) {
	t.Parallel()

	auth := NewStubAuthenticator("secret", "", "token-abc")
	srv := testNewServer(auth, "wrong@example.com", func(string) { t.Fatal("不应写入 session") }, nil)
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.PostForm(ts.URL+"/unlock", url.Values{
		"email":    {"retry@example.com"},
		"password": {"wrong"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "主密码不正确") {
		t.Fatalf("期望密码错误提示: %s", body)
	}
	if !strings.Contains(html, `value="retry@example.com"`) {
		t.Fatalf("错误后应保留用户输入的 email: %s", html)
	}
}

func TestServerUnlock_SavesEmailToConfig(t *testing.T) {
	t.Parallel()

	decHome := t.TempDir()
	configPath := filepath.Join(decHome, "secrets", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	initial := "server_url: https://vault.example.com\n"
	if err := os.WriteFile(configPath, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}

	auth := NewStubAuthenticator("secret", "", "token-save")
	srv := testNewServer(auth, "", func(string) {}, func(email string) error {
		type cfg struct {
			ServerURL string `yaml:"server_url"`
			Email     string `yaml:"email"`
		}
		c := cfg{ServerURL: "https://vault.example.com"}
		data, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(data, &c); err != nil {
			return err
		}
		c.Email = email
		out, err := yaml.Marshal(&c)
		if err != nil {
			return err
		}
		return os.WriteFile(configPath, out, 0600)
	})
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.PostForm(ts.URL+"/unlock", url.Values{
		"email":    {"persist@example.com"},
		"password": {"secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		ServerURL string `yaml:"server_url"`
		Email     string `yaml:"email"`
	}
	if err := yaml.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Email != "persist@example.com" {
		t.Fatalf("email = %q, 期望 persist@example.com", saved.Email)
	}
	if saved.ServerURL != "https://vault.example.com" {
		t.Fatalf("server_url 被覆盖: %q", saved.ServerURL)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("config 权限 = %o, 期望 0600", info.Mode().Perm())
	}
}

func TestServerUnlock_2FAFlow(t *testing.T) {
	t.Parallel()

	var session atomic.Value
	auth := NewStubAuthenticator("secret", "654321", "token-2fa")
	srv := testNewServer(auth, "", func(token string) {
		session.Store(token)
	}, nil)
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp1, err := client.PostForm(ts.URL+"/unlock", url.Values{
		"email":    {"user@example.com"},
		"password": {"secret"},
	})
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
	html3 := string(body3)
	if !strings.Contains(html3, "解锁成功") {
		t.Fatalf("期望成功页: %s", html3)
	}
	if !strings.Contains(html3, "window.close") {
		t.Fatalf("成功页缺少自动关窗逻辑: %s", html3)
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
			resp, postErr := http.PostForm(openURL, url.Values{
				"email":    {"user@example.com"},
				"password": {"pw"},
			})
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
			resp, postErr := http.PostForm(unlockURL, url.Values{
				"email":    {"user@example.com"},
				"password": {"pw"},
			})
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
			resp, postErr := http.PostForm(openURL, url.Values{
				"email":    {"user@example.com"},
				"password": {"pw"},
			})
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
		OpenBrowser:   func(string) error { return nil },
	})
	if err == nil {
		t.Fatal("Run() 应返回取消错误")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %v, 期望 context.Canceled", err)
	}
}

func TestRun_WebUnlockTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	statuses := []string{}
	err := Run(ctx, Options{
		Authenticator: NewStubAuthenticator("pw", "", "sess-timeout"),
		OpenBrowser:   func(string) error { return nil },
		OnStatus: func(message string) {
			statuses = append(statuses, message)
		},
	})
	if err == nil {
		t.Fatal("Run() 应超时失败")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() = %v, 期望 context.DeadlineExceeded", err)
	}
	found := false
	for _, s := range statuses {
		if strings.Contains(s, "timeout - no user input") {
			found = true
		}
	}
	if !found {
		t.Fatalf("未记录超时日志: %v", statuses)
	}
}

func TestWebUnlockTimeout_EnvOverride(t *testing.T) {
	t.Setenv("DEC_BW_UNLOCK_TIMEOUT", "90s")
	if got := WebUnlockTimeout(); got != 90*time.Second {
		t.Fatalf("WebUnlockTimeout() = %v, 期望 90s", got)
	}
	t.Setenv("DEC_BW_UNLOCK_TIMEOUT", "invalid")
	if got := WebUnlockTimeout(); got != DefaultWebUnlockTimeout {
		t.Fatalf("WebUnlockTimeout() = %v, 期望默认 %v", got, DefaultWebUnlockTimeout)
	}
}
