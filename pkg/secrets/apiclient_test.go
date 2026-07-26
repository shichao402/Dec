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

func TestAPIClient_PushBundle_CreateSecureNotePayload(t *testing.T) {
	t.Parallel()

	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{{ID: "f1", Name: "vikunja_workflow"}},
			})
		case "/api/ciphers":
			switch r.Method {
			case http.MethodGet:
				_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{Data: nil})
			case http.MethodPost:
				data, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if err := json.Unmarshal(data, &createBody); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusOK)
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{ServerURL: srv.URL}
	client, err := NewAPIClient(cfg, "sess-push", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	userKey := bytes.Repeat([]byte{0x03}, 64)
	SetUserKey(userKey)
	t.Cleanup(ClearSession)

	noteName := "mise/conf.d/vikunja.toml"
	content := "[env]\nTOKEN=abc\n"
	result, err := client.PushBundle(context.Background(), PushBundleRequest{
		DecBundleName: "vikunja",
		Binding:       BundleBinding{SecretsBundleName: "vikunja_workflow"},
	}, []SecureNote{{RelativePath: noteName, Content: content}})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if result.Created != 1 || result.Updated != 0 {
		t.Fatalf("result = %#v", result)
	}
	if createBody == nil {
		t.Fatal("未收到 POST /ciphers 请求")
	}
	if createBody["type"] != float64(cipherTypeSecureNote) {
		t.Fatalf("type = %v", createBody["type"])
	}
	secureNote, ok := createBody["secureNote"].(map[string]any)
	if !ok || secureNote["type"] != float64(0) {
		t.Fatalf("secureNote = %#v", createBody["secureNote"])
	}
	key, _ := createBody["key"].(string)
	if key == "" || !looksEncrypted(key) {
		t.Fatalf("key 应为用户 key 加密的 cipher key: %q", key)
	}
	name, _ := createBody["name"].(string)
	notes, _ := createBody["notes"].(string)
	if !looksEncrypted(name) || !looksEncrypted(notes) {
		t.Fatalf("name/notes 应加密: name=%q notes=%q", name, notes)
	}
	itemKey, err := itemDecryptionKey(key, userKey)
	if err != nil {
		t.Fatal(err)
	}
	gotName, err := decryptVaultString(name, itemKey)
	if err != nil {
		t.Fatal(err)
	}
	if gotName != noteName {
		t.Fatalf("解密 name = %q, want %q", gotName, noteName)
	}
	gotNotes, err := decryptVaultString(notes, itemKey)
	if err != nil {
		t.Fatal(err)
	}
	if gotNotes != content {
		t.Fatalf("解密 notes = %q, want %q", gotNotes, content)
	}
}

func TestAPIClient_PushBundle_UpdatePreservesCipherKey(t *testing.T) {
	// 不使用 t.Parallel：本测试与全局 UserKey 状态交互，并行 cleanup 会干扰。

	userKey := bytes.Repeat([]byte{0x05}, 64)
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
	encNotes, err := encryptVaultString("[env]\nOLD=1", itemKey)
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
			// 模拟 Bitwarden：未回传 key 时清空 cipher key。
			if key, _ := updateBody["key"].(string); strings.TrimSpace(key) == "" {
				ciphers[0].Key = ""
			}
			if name, ok := updateBody["name"].(string); ok {
				ciphers[0].Name = name
			}
			if notes, ok := updateBody["notes"].(string); ok {
				ciphers[0].Notes = notes
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{ServerURL: srv.URL}
	client, err := NewAPIClient(cfg, "sess-push", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	SetUserKey(userKey)
	t.Cleanup(ClearSession)

	newContent := "[env]\nNEW=1"
	result, err := client.PushBundle(context.Background(), PushBundleRequest{
		DecBundleName: "vikunja",
		Binding:       BundleBinding{SecretsBundleName: "vikunja_workflow"},
	}, []SecureNote{{RelativePath: legacyName, Content: newContent}})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if result.Created != 0 || result.Updated != 1 {
		t.Fatalf("result = %#v", result)
	}
	key, _ := updateBody["key"].(string)
	if key != encItemKey {
		t.Fatalf("update 应回传原有 cipher key: got %q want %q", key, encItemKey)
	}

	pullResult, err := client.PullBundle(context.Background(), PullBundleRequest{
		DecBundleName: "vikunja",
		Binding:       BundleBinding{SecretsBundleName: "vikunja_workflow"},
	})
	if err != nil {
		t.Fatalf("PullBundle() after update = %v", err)
	}
	if len(pullResult.Notes) != 1 {
		t.Fatalf("Notes = %#v", pullResult.Notes)
	}
	// push 只改正文，远端 note 名保持原样——否则迁移期间会把改好的消费者路径名改回去。
	if pullResult.Notes[0].RelativePath != legacyName {
		t.Fatalf("RelativePath = %q, want %q（push 不应改远端 note 名）", pullResult.Notes[0].RelativePath, legacyName)
	}
	if gotName, _ := updateBody["name"].(string); gotName != encName {
		t.Fatalf("update 应原样回传远端 name 密文: got %q want %q", gotName, encName)
	}
	if pullResult.Notes[0].Content != newContent {
		t.Fatalf("Content = %q, want %q", pullResult.Notes[0].Content, newContent)
	}
}

// note 名只有一种合法形态（项目根相对落地路径），匹配按精确名走。
func TestAPIClient_PushBundle_UpdateMatchesNameExactly(t *testing.T) {
	t.Parallel()

	var updatedID string
	var updateBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch {
		case r.URL.Path == "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{{ID: "f1", Name: "vikunja_workflow"}},
			})
		case r.URL.Path == "/api/ciphers" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{
				Data: []bwCipher{{
					ID:       "cipher-legacy",
					Type:     cipherTypeSecureNote,
					Name:     ".config/mise/conf.d/vikunja.toml",
					Notes:    "[env]\nOLD=1",
					FolderID: "f1",
				}},
			})
		case strings.HasPrefix(r.URL.Path, "/api/ciphers/") && r.Method == http.MethodPut:
			updatedID = strings.TrimPrefix(r.URL.Path, "/api/ciphers/")
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := json.Unmarshal(data, &updateBody); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{ServerURL: srv.URL}
	client, err := NewAPIClient(cfg, "sess-push", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	SetUserKey(bytes.Repeat([]byte{0x04}, 64))
	t.Cleanup(ClearSession)

	result, err := client.PushBundle(context.Background(), PushBundleRequest{
		DecBundleName: "vikunja",
		Binding:       BundleBinding{SecretsBundleName: "vikunja_workflow"},
	}, []SecureNote{{RelativePath: ".config/mise/conf.d/vikunja.toml", Content: "[env]\nNEW=1"}})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if result.Created != 0 || result.Updated != 1 {
		t.Fatalf("result = %#v", result)
	}
	if updatedID != "cipher-legacy" {
		t.Fatalf("updatedID = %q", updatedID)
	}
	secureNote, ok := updateBody["secureNote"].(map[string]any)
	if !ok || secureNote["type"] != float64(0) {
		t.Fatalf("secureNote = %#v", updateBody["secureNote"])
	}
}

func TestFormatAPIError(t *testing.T) {
	t.Parallel()

	msg := formatAPIError(400, []byte(`{"message":"Invalid cipher","validationErrors":{"Name":["too long"]}}`))
	if !strings.Contains(msg, "HTTP 400") || !strings.Contains(msg, "Invalid cipher") || !strings.Contains(msg, "Name:") {
		t.Fatalf("formatAPIError() = %q", msg)
	}
}
