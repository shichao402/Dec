package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityClient_LoginSuccess(t *testing.T) {
	t.Parallel()

	var gotPassword string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/accounts/prelogin":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			_ = json.NewEncoder(w).Encode(preloginResponse{KdfIterations: 1000})
		case "/connect/token":
			_ = r.ParseForm()
			gotPassword = r.FormValue("password")
			_ = json.NewEncoder(w).Encode(tokenSuccessResponse{AccessToken: "access-token"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := &IdentityClient{
		IdentityURL: srv.URL,
		Email:       "user@example.com",
		DeviceID:    "device-1",
		HTTP:        srv.Client(),
	}

	attempt, err := client.Login(context.Background(), "master-password", "", "", "", LoginOptions{})
	if err != nil {
		t.Fatalf("Login() = %v", err)
	}
	if attempt.need2FA {
		t.Fatal("不应需要 2FA")
	}
	if attempt.accessToken != "access-token" {
		t.Fatalf("accessToken = %q", attempt.accessToken)
	}
	wantHash := masterPasswordHash("master-password", "user@example.com", 1000)
	if gotPassword != wantHash {
		t.Fatalf("password hash = %q, 期望 %q", gotPassword, wantHash)
	}
}

func TestIdentityClient_LoginRequires2FA(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/accounts/prelogin":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			_ = json.NewEncoder(w).Encode(preloginResponse{KdfIterations: 1000})
		case "/connect/token":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(tokenErrorResponse{
				TwoFactorToken:     "challenge-token",
				TwoFactorProviders: []string{"0"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := &IdentityClient{
		IdentityURL: srv.URL,
		Email:       "user@example.com",
		DeviceID:    "device-1",
		HTTP:        srv.Client(),
	}
	attempt, err := client.Login(context.Background(), "master-password", "", "", "", LoginOptions{})
	if err != nil {
		t.Fatalf("Login() = %v", err)
	}
	if !attempt.need2FA || attempt.twoFactorSession != "challenge-token" {
		t.Fatalf("attempt = %#v", attempt)
	}
}

func TestDescribeLoginFailure(t *testing.T) {
	t.Parallel()

	newDevice := describeLoginFailure(http.StatusBadRequest, "New device verification required.")
	for _, want := range []string{"New device verification required.", "两步登录", "Danger Zone"} {
		if !strings.Contains(newDevice, want) {
			t.Fatalf("新设备验证提示缺少 %q:\n%s", want, newDevice)
		}
	}

	throttled := describeLoginFailure(http.StatusTooManyRequests, "Slow down!")
	if !strings.Contains(throttled, "频率限制") {
		t.Fatalf("限流提示 = %q", throttled)
	}

	// 未识别的错误保持原样，不要凭空编造建议。
	if got := describeLoginFailure(http.StatusBadRequest, "Username or password is incorrect."); got != "Username or password is incorrect." {
		t.Fatalf("未知错误不应被改写: %q", got)
	}
}

func TestIdentityClient_LoginNewDeviceVerificationHint(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/accounts/prelogin":
			_ = json.NewEncoder(w).Encode(preloginResponse{KdfIterations: 1000})
		case "/connect/token":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":             "invalid_grant",
				"error_description": "New device verification required.",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := &IdentityClient{
		IdentityURL: srv.URL,
		Email:       "user@example.com",
		DeviceID:    "device-1",
		HTTP:        srv.Client(),
	}
	_, err := client.Login(context.Background(), "master-password", "", "", "", LoginOptions{})
	if err == nil {
		t.Fatal("Login() 应失败")
	}
	if !strings.Contains(err.Error(), "两步登录") {
		t.Fatalf("错误缺少新设备验证处置建议:\n%v", err)
	}
}

func TestIdentityClient_LoginRequires2FAWithoutChallengeToken(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/accounts/prelogin":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			_ = json.NewEncoder(w).Encode(preloginResponse{KdfIterations: 1000})
		case "/connect/token":
			if r.FormValue("twoFactorToken") == "123456" {
				_ = json.NewEncoder(w).Encode(tokenSuccessResponse{AccessToken: "after-2fa"})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":              "invalid_grant",
				"error_description":  "Two factor required.",
				"TwoFactorProviders": []string{"0"},
				"TwoFactorProviders2": map[string]any{
					"0": nil,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := &IdentityClient{
		IdentityURL: srv.URL,
		Email:       "user@example.com",
		DeviceID:    "device-1",
		HTTP:        srv.Client(),
	}
	attempt, err := client.Login(context.Background(), "master-password", "", "", "", LoginOptions{})
	if err != nil {
		t.Fatalf("Login() = %v", err)
	}
	if !attempt.need2FA || attempt.twoFactorSession != "" || attempt.twoFactorProvider != "0" {
		t.Fatalf("attempt = %#v", attempt)
	}

	attempt, err = client.Login(context.Background(), "master-password", "123456", "", "0", LoginOptions{})
	if err != nil {
		t.Fatalf("Login(2fa) = %v", err)
	}
	if attempt.accessToken != "after-2fa" {
		t.Fatalf("accessToken = %q", attempt.accessToken)
	}
}

func TestBWAuthenticator_2FAFlowWithoutChallengeToken(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/identity/accounts/prelogin":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			_ = json.NewEncoder(w).Encode(preloginResponse{KdfIterations: 1000})
		case "/identity/connect/token":
			_ = r.ParseForm()
			if r.FormValue("twoFactorToken") == "123456" {
				if r.FormValue("token") != "" {
					http.Error(w, "unexpected challenge token", http.StatusBadRequest)
					return
				}
				if r.FormValue("twoFactorProvider") != "0" {
					http.Error(w, "unexpected provider", http.StatusBadRequest)
					return
				}
				_ = json.NewEncoder(w).Encode(tokenSuccessResponse{AccessToken: "after-2fa"})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":              "invalid_grant",
				"error_description":  "Two factor required.",
				"TwoFactorProviders": []string{"0"},
				"TwoFactorProviders2": map[string]any{
					"0": nil,
				},
			})
		case "/api/accounts/profile":
			mockProfileHandler(w, "master-password", "user@example.com", 1000)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{
		ServerURL: srv.URL,
		Email:     "user@example.com",
	}
	auth, err := NewBWAuthenticator(cfg, "device-1", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	token, need2FA, err := auth.Unlock(context.Background(), "user@example.com", "master-password")
	if err != nil || !need2FA || token != "" {
		t.Fatalf("Unlock() = (%q, %v, %v)", token, need2FA, err)
	}
	token, err = auth.Verify2FA(context.Background(), "123456", true)
	if err != nil {
		t.Fatalf("Verify2FA() = %v", err)
	}
	if token != "after-2fa" {
		t.Fatalf("token = %q", token)
	}
}

func TestBWAuthenticator_2FAFlow(t *testing.T) {
	t.Parallel()

	var sawChallenge bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/identity/accounts/prelogin":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			_ = json.NewEncoder(w).Encode(preloginResponse{KdfIterations: 1000})
		case "/identity/connect/token":
			_ = r.ParseForm()
			if r.FormValue("twoFactorToken") == "123456" {
				if r.FormValue("token") != "challenge-token" {
					http.Error(w, "missing challenge token", http.StatusBadRequest)
					return
				}
				sawChallenge = true
				_ = json.NewEncoder(w).Encode(tokenSuccessResponse{AccessToken: "after-2fa"})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(tokenErrorResponse{
				TwoFactorToken:     "challenge-token",
				TwoFactorProviders: []string{"0"},
			})
		case "/api/accounts/profile":
			mockProfileHandler(w, "master-password", "user@example.com", 1000)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{
		ServerURL: srv.URL,
		Email:     "user@example.com",
	}
	auth, err := NewBWAuthenticator(cfg, "device-1", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	token, need2FA, err := auth.Unlock(context.Background(), "user@example.com", "master-password")
	if err != nil || !need2FA || token != "" {
		t.Fatalf("Unlock() = (%q, %v, %v)", token, need2FA, err)
	}
	token, err = auth.Verify2FA(context.Background(), "123456", true)
	if err != nil {
		t.Fatalf("Verify2FA() = %v", err)
	}
	if token != "after-2fa" {
		t.Fatalf("token = %q", token)
	}
	if !sawChallenge {
		t.Fatal("2FA 请求应携带 challenge token")
	}
}

func TestAPIClient_PullBundleByFolder(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{{ID: "f1", Name: "vikunja"}},
			})
		case "/api/ciphers":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer sess-") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{
				Data: []bwCipher{
					{Type: 1, Name: "login", FolderID: "f1"},
					{Type: cipherTypeSecureNote, Name: "env/vikunja.env", Notes: "VIKUNJA_API_TOKEN=abc\n", FolderID: "f1"},
					{Type: cipherTypeSecureNote, Name: "other.toml", Notes: "skip", FolderID: "f2"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{ServerURL: srv.URL}
	client, err := NewAPIClient(cfg, "sess-abc", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(ClearSession)
	result, err := client.PullBundle(context.Background(), PullBundleRequest{
		DecBundleName: "vikunja",
		Binding:       BundleBinding{SecretsBundleName: "vikunja"},
	})
	if err != nil {
		t.Fatalf("PullBundle() = %v", err)
	}
	if len(result.Notes) != 1 {
		t.Fatalf("Notes = %#v", result.Notes)
	}
	if result.Notes[0].RelativePath != "env/vikunja.env" {
		t.Fatalf("RelativePath = %q", result.Notes[0].RelativePath)
	}
}

func TestDefaultClient_UsesAPIWhenSessionPresent(t *testing.T) {
	ClearSession()
	t.Cleanup(ClearSession)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{Data: nil})
		case "/api/ciphers":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{Data: nil})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("server_url: "+srv.URL+"\nemail: user@example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origFactory := httpClientFactory
	httpClientFactory = func() *http.Client { return srv.Client() }
	t.Cleanup(func() { httpClientFactory = origFactory })

	SetSession("sess-test")
	SetUserKey(bytes.Repeat([]byte{0x02}, 64))
	client := DefaultClient()
	if _, ok := client.(NoopClient); ok {
		t.Fatal("有 session 且已配置时应返回 APIClient")
	}
	result, err := client.PullBundle(context.Background(), PullBundleRequest{DecBundleName: "x"})
	if err != nil {
		t.Fatalf("PullBundle() = %v, 期望 folder 不存在时返回空结果", err)
	}
	if result == nil || len(result.Notes) != 0 {
		t.Fatalf("PullBundle() = %#v, 期望空 Notes", result)
	}
}

func TestConfig_Endpoints(t *testing.T) {
	t.Parallel()

	cases := []struct {
		serverURL string
		identity  string
		api       string
	}{
		{"https://vault.bitwarden.com", "https://identity.bitwarden.com", "https://api.bitwarden.com"},
		{"https://bw.example.com", "https://bw.example.com/identity", "https://bw.example.com/api"},
	}
	for _, tc := range cases {
		cfg := &Config{ServerURL: tc.serverURL}
		identity, api, err := cfg.Endpoints()
		if err != nil {
			t.Fatalf("Endpoints(%q) = %v", tc.serverURL, err)
		}
		if identity != tc.identity || api != tc.api {
			t.Fatalf("Endpoints(%q) = (%q, %q), 期望 (%q, %q)", tc.serverURL, identity, api, tc.identity, tc.api)
		}
	}
}

func TestIdentityClient_LoginSkips2FAWithRememberToken(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/accounts/prelogin":
			_ = json.NewEncoder(w).Encode(preloginResponse{KdfIterations: 1000})
		case "/connect/token":
			_ = r.ParseForm()
			if r.FormValue("twoFactorProvider") != twoFactorProviderRemember {
				t.Fatalf("twoFactorProvider = %q", r.FormValue("twoFactorProvider"))
			}
			if r.FormValue("twoFactorToken") != "remember-token" {
				t.Fatalf("twoFactorToken = %q", r.FormValue("twoFactorToken"))
			}
			if r.FormValue("deviceIdentifier") != "device-trusted" {
				t.Fatalf("deviceIdentifier = %q", r.FormValue("deviceIdentifier"))
			}
			_ = json.NewEncoder(w).Encode(tokenSuccessResponse{AccessToken: "access-with-remember"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := &IdentityClient{
		IdentityURL: srv.URL,
		Email:       "user@example.com",
		DeviceID:    "device-trusted",
		HTTP:        srv.Client(),
	}
	attempt, err := client.Login(context.Background(), "master-password", "", "", "", LoginOptions{
		RememberToken: "remember-token",
	})
	if err != nil {
		t.Fatalf("Login() = %v", err)
	}
	if attempt.need2FA {
		t.Fatal("有 remember token 时不应需要 2FA")
	}
	if attempt.accessToken != "access-with-remember" {
		t.Fatalf("accessToken = %q", attempt.accessToken)
	}
}

func TestIdentityClient_LoginRequires2FAWithoutDeviceRemember(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/accounts/prelogin":
			_ = json.NewEncoder(w).Encode(preloginResponse{KdfIterations: 1000})
		case "/connect/token":
			_ = r.ParseForm()
			if r.FormValue("twoFactorToken") != "" {
				http.Error(w, "unexpected 2fa token", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(tokenErrorResponse{
				TwoFactorToken:     "challenge-token",
				TwoFactorProviders: []string{"0"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := &IdentityClient{
		IdentityURL: srv.URL,
		Email:       "user@example.com",
		DeviceID:    "new-device",
		HTTP:        srv.Client(),
	}
	attempt, err := client.Login(context.Background(), "master-password", "", "", "", LoginOptions{})
	if err != nil {
		t.Fatalf("Login() = %v", err)
	}
	if !attempt.need2FA {
		t.Fatal("新设备应要求 2FA")
	}
}

func TestIdentityClient_LoginRememberDeviceSendsFlag(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/accounts/prelogin":
			_ = json.NewEncoder(w).Encode(preloginResponse{KdfIterations: 1000})
		case "/connect/token":
			_ = r.ParseForm()
			if r.FormValue("twoFactorRemember") != "1" {
				t.Fatalf("twoFactorRemember = %q", r.FormValue("twoFactorRemember"))
			}
			_ = json.NewEncoder(w).Encode(tokenSuccessResponse{
				AccessToken:    "access-after-2fa",
				TwoFactorToken: "new-remember-token",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := &IdentityClient{
		IdentityURL: srv.URL,
		Email:       "user@example.com",
		DeviceID:    "device-1",
		HTTP:        srv.Client(),
	}
	attempt, err := client.Login(context.Background(), "master-password", "123456", "challenge", "0", LoginOptions{
		RememberDevice: true,
	})
	if err != nil {
		t.Fatalf("Login() = %v", err)
	}
	if attempt.twoFactorRemember != "new-remember-token" {
		t.Fatalf("twoFactorRemember token = %q", attempt.twoFactorRemember)
	}
}

func TestBWAuthenticator_UnlockUsesStoredRememberToken(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	deviceJSON := `{"identifier":"device-trusted","two_factor_remember":{"user@example.com":"remember-token"}}`
	if err := os.WriteFile(filepath.Join(secretsDir, "device.json"), []byte(deviceJSON), 0600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/identity/accounts/prelogin":
			_ = json.NewEncoder(w).Encode(preloginResponse{KdfIterations: 1000})
		case "/identity/connect/token":
			_ = r.ParseForm()
			if r.FormValue("twoFactorProvider") == twoFactorProviderRemember {
				_ = json.NewEncoder(w).Encode(tokenSuccessResponse{AccessToken: "access-no-2fa"})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(tokenErrorResponse{TwoFactorProviders: []string{"0"}})
		case "/api/accounts/profile":
			mockProfileHandler(w, "master-password", "user@example.com", 1000)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{ServerURL: srv.URL, Email: "user@example.com"}
	auth, err := NewBWAuthenticator(cfg, "", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	token, need2FA, err := auth.Unlock(context.Background(), "user@example.com", "master-password")
	if err != nil || need2FA || token != "access-no-2fa" {
		t.Fatalf("Unlock() = (%q, %v, %v)", token, need2FA, err)
	}
}

func mockProfileHandler(w http.ResponseWriter, password, email string, iterations int) {
	key, err := testEncryptedUserKey(password, email, iterations)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(bwProfile{Key: key})
}
