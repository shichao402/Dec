package secrets

import (
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
		switch r.URL.Path {
		case "/accounts/prelogin":
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

	attempt, err := client.Login(context.Background(), "master-password", "", "")
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
		switch r.URL.Path {
		case "/accounts/prelogin":
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
	attempt, err := client.Login(context.Background(), "master-password", "", "")
	if err != nil {
		t.Fatalf("Login() = %v", err)
	}
	if !attempt.need2FA || attempt.twoFactorSession != "challenge-token" {
		t.Fatalf("attempt = %#v", attempt)
	}
}

func TestBWAuthenticator_2FAFlow(t *testing.T) {
	t.Parallel()

	var sawChallenge bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/identity/accounts/prelogin":
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
	token, need2FA, err := auth.Unlock(context.Background(), "master-password")
	if err != nil || !need2FA || token != "" {
		t.Fatalf("Unlock() = (%q, %v, %v)", token, need2FA, err)
	}
	token, err = auth.Verify2FA(context.Background(), "123456")
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
		switch r.URL.Path {
		case "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{{ID: "f1", Name: "vikunja_workflow"}},
			})
		case "/api/ciphers":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer sess-") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{
				Data: []bwCipher{
					{Type: 1, Name: "login", FolderID: "f1"},
					{Type: cipherTypeSecureNote, Name: ".config/mise/conf.d/vikunja.toml", Notes: "[env]\nX=1", FolderID: "f1"},
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
	result, err := client.PullBundle(context.Background(), PullBundleRequest{
		DecBundleName: "vikunja",
		Binding:       BundleBinding{BitwardenFolder: "vikunja_workflow"},
	})
	if err != nil {
		t.Fatalf("PullBundle() = %v", err)
	}
	if len(result.Notes) != 1 {
		t.Fatalf("Notes = %#v", result.Notes)
	}
	if result.Notes[0].RelativePath != ".config/mise/conf.d/vikunja.toml" {
		t.Fatalf("RelativePath = %q", result.Notes[0].RelativePath)
	}
}

func TestDefaultClient_UsesAPIWhenSessionPresent(t *testing.T) {
	ClearSession()
	t.Cleanup(ClearSession)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	client := DefaultClient()
	if _, ok := client.(NoopClient); ok {
		t.Fatal("有 session 且已配置时应返回 APIClient")
	}
	_, err := client.PullBundle(context.Background(), PullBundleRequest{DecBundleName: "x"})
	if err != nil {
		t.Fatalf("PullBundle() = %v", err)
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
