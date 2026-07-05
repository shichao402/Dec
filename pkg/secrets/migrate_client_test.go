package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIClient_MigrateBundle_RenamesLegacyNote(t *testing.T) {
	t.Parallel()

	userKey := bytes.Repeat([]byte{0x06}, 64)
	itemKey, err := generateCipherKey()
	if err != nil {
		t.Fatal(err)
	}
	encItemKey, err := encryptVaultBytes(itemKey, userKey)
	if err != nil {
		t.Fatal(err)
	}
	legacyName := ".config/mise/conf.d/vikunja.toml"
	encName, err := encryptVaultString(legacyName, itemKey)
	if err != nil {
		t.Fatal(err)
	}
	encNotes, err := encryptVaultString("[env]\nTOKEN=abc\n", itemKey)
	if err != nil {
		t.Fatal(err)
	}

	ciphers := []bwCipher{{
		ID:       "cipher-legacy",
		Type:     cipherTypeSecureNote,
		Name:     encName,
		Notes:    encNotes,
		FolderID: "f1",
		Key:      encItemKey,
	}}

	var updateBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch {
		case r.URL.Path == "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{{ID: "f1", Name: "vikunja_workflow"}},
			})
		case r.URL.Path == "/api/ciphers" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{Data: ciphers})
		case strings.HasPrefix(r.URL.Path, "/api/ciphers/") && r.Method == http.MethodPut:
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := json.Unmarshal(data, &updateBody); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if name, ok := updateBody["name"].(string); ok {
				ciphers[0].Name = name
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{ServerURL: srv.URL}
	client, err := NewAPIClient(cfg, "sess-migrate", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	SetUserKey(userKey)
	t.Cleanup(ClearSession)

	result, err := client.MigrateBundle(context.Background(), MigrateBundleRequest{
		DecBundleName: "vikunja",
		Binding:       BundleBinding{SecretsBundleName: "vikunja_workflow"},
	})
	if err != nil {
		t.Fatalf("MigrateBundle() = %v", err)
	}
	if len(result.RenamedNotes) != 1 {
		t.Fatalf("RenamedNotes = %#v", result.RenamedNotes)
	}
	want := legacyName + " → .secrets/vikunja_workflow/mise/conf.d/vikunja.toml"
	if result.RenamedNotes[0] != want {
		t.Fatalf("RenamedNotes[0] = %q, want %q", result.RenamedNotes[0], want)
	}
	key, _ := updateBody["key"].(string)
	if key != encItemKey {
		t.Fatalf("rename 应保留 cipher key: got %q want %q", key, encItemKey)
	}
}

func TestAPIClient_MigrateBundle_SkipsUndecryptableCipher(t *testing.T) {
	t.Parallel()

	userKey := bytes.Repeat([]byte{0x07}, 64)
	ciphers := []bwCipher{{
		ID:       "cipher-broken",
		Type:     cipherTypeSecureNote,
		Name:     "invalid-ciphertext",
		Notes:    "invalid",
		FolderID: "f1",
		Key:      "invalid-key",
	}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{{ID: "f1", Name: "vikunja_workflow"}},
			})
		case "/api/ciphers":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{Data: ciphers})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{ServerURL: srv.URL}
	client, err := NewAPIClient(cfg, "sess-migrate", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	SetUserKey(userKey)
	t.Cleanup(ClearSession)

	result, err := client.MigrateBundle(context.Background(), MigrateBundleRequest{
		DecBundleName: "vikunja",
		Binding:       BundleBinding{SecretsBundleName: "vikunja_workflow"},
	})
	if err != nil {
		t.Fatalf("MigrateBundle() = %v", err)
	}
	if len(result.SkippedCiphers) != 1 || result.SkippedCiphers[0] != "cipher-broken" {
		t.Fatalf("SkippedCiphers = %#v", result.SkippedCiphers)
	}
}
