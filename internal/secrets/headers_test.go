package secrets

import (
	"net/http"
	"testing"
)

func TestApplyBitwardenHeaders(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodGet, "https://identity.example.com/accounts/prelogin", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyBitwardenHeaders(req)

	assertHeader := func(name, want string) {
		t.Helper()
		if got := req.Header.Get(name); got != want {
			t.Fatalf("%s = %q, 期望 %q", name, got, want)
		}
	}
	assertHeader("Bitwarden-Client-Version", bitwardenClientVersion)
	assertHeader("Bitwarden-Client-Name", bitwardenClientName)
	assertHeader("Device-Type", bitwardenDeviceType)
	assertHeader("User-Agent", "Dec/"+bitwardenClientVersion)
	assertHeader("Cache-Control", "no-store")
	assertHeader("Pragma", "no-cache")
}

func assertBitwardenHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Bitwarden-Client-Version"); got != bitwardenClientVersion {
		t.Fatalf("Bitwarden-Client-Version = %q, 期望 %q", got, bitwardenClientVersion)
	}
	if got := r.Header.Get("Bitwarden-Client-Name"); got != bitwardenClientName {
		t.Fatalf("Bitwarden-Client-Name = %q, 期望 %q", got, bitwardenClientName)
	}
	if got := r.Header.Get("Device-Type"); got != bitwardenDeviceType {
		t.Fatalf("Device-Type = %q, 期望 %q", got, bitwardenDeviceType)
	}
	if got := r.Header.Get("User-Agent"); got != "Dec/"+bitwardenClientVersion {
		t.Fatalf("User-Agent = %q, 期望 %q", got, "Dec/"+bitwardenClientVersion)
	}
}
