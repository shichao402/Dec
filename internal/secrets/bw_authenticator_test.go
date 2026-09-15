package secrets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 失效的记住设备令牌必须能自愈：它存在 device.json 里，每次登录都会被带上，
// 服务端直接报错而不是要求 2FA 时，用户会被永久挡在验证码之前。
func TestBWAuthenticator_UnlockRetriesWithoutStaleRememberToken(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	const email = "alice@dec.test"
	if err := SetRememberToken(email, "stale-token"); err != nil {
		t.Fatal(err)
	}

	var withRemember, withoutRemember int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/accounts/prelogin":
			_ = json.NewEncoder(w).Encode(preloginResponse{KdfIterations: 1000})
		case "/connect/token":
			_ = r.ParseForm()
			w.WriteHeader(http.StatusBadRequest)
			if r.FormValue("twoFactorToken") == "stale-token" {
				withRemember++
				_ = json.NewEncoder(w).Encode(tokenErrorResponse{
					Error:            "invalid_grant",
					ErrorDescription: "Device trust is no longer valid.",
				})
				return
			}
			withoutRemember++
			_ = json.NewEncoder(w).Encode(tokenErrorResponse{
				TwoFactorToken:     "challenge-token",
				TwoFactorProviders: []string{"0"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	auth := &BWAuthenticator{
		cfg:   &Config{ServerURL: DefaultServerURL, Email: email},
		email: email,
		client: &IdentityClient{
			IdentityURL: srv.URL,
			Email:       email,
			DeviceID:    "device-1",
			HTTP:        srv.Client(),
		},
	}

	token, need2FA, err := auth.Unlock(context.Background(), email, "master-password")
	if err != nil {
		t.Fatalf("Unlock() = %v", err)
	}
	if !need2FA {
		t.Fatal("令牌失效后应回退到要求 2FA")
	}
	if token != "" {
		t.Fatalf("token = %q", token)
	}
	if withRemember != 1 || withoutRemember != 1 {
		t.Fatalf("请求次数 = remember:%d plain:%d", withRemember, withoutRemember)
	}
	stored, err := RememberToken(email)
	if err != nil {
		t.Fatal(err)
	}
	if stored != "" {
		t.Fatal("失效令牌应已从 device.json 清除")
	}
}

// Bitwarden 对失效的记住设备令牌回 "Two-step token is invalid"，措辞是 two-step
// 而不是 two-factor，漏掉就会被当成普通登录失败。
func TestTokenErrorResponse_TwoStepDescriptionRequires2FA(t *testing.T) {
	resp := tokenErrorResponse{
		Error:            "invalid_grant",
		ErrorDescription: "Two-step token is invalid. Try again.",
	}
	if !resp.requires2FA() {
		t.Fatal("two-step 描述应识别为需要 2FA")
	}
}
