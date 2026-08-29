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

// declaredPTarget 构造声明型项目目标：Bitwarden folder 只有项目名一级。
func declaredPTarget(t testing.TB, pName string, plane SyncPlane) SyncTarget {
	t.Helper()
	target, err := NewPSyncTarget(pName, plane)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

// projectItemName 返回 project 平面下 rel 对应的 Bitwarden 条目名。
func projectItemName(t testing.TB, rel string) string {
	t.Helper()
	scope, err := NewRemoteScope("p", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	name, err := scope.encodeItemName(rel)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func TestAPIClient_ReauthenticatesOnceAfterUnauthorized(t *testing.T) {
	ClearSession()
	SetSession("stale-session")
	t.Cleanup(ClearSession)

	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.Header.Get("Authorization") {
		case "Bearer stale-session":
			http.Error(w, "expired", http.StatusUnauthorized)
		case "Bearer fresh-session":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{})
		default:
			http.Error(w, "unexpected token", http.StatusUnauthorized)
		}
	}))
	t.Cleanup(srv.Close)

	oldReauthenticate := reauthenticateSession
	reauthenticateSession = func(context.Context) error {
		SetSession("fresh-session")
		return nil
	}
	t.Cleanup(func() { reauthenticateSession = oldReauthenticate })

	client, err := NewAPIClient(&Config{ServerURL: srv.URL}, "stale-session", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	var out bwListResponse[bwFolder]
	if err := client.getJSON(context.Background(), srv.URL, &out); err != nil {
		t.Fatalf("getJSON() = %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if Session() != "fresh-session" || client.Token != "fresh-session" {
		t.Fatalf("session=%q client.Token=%q", Session(), client.Token)
	}
}

func TestAPIClient_DoesNotLoopWhenFreshSessionIsUnauthorized(t *testing.T) {
	ClearSession()
	SetSession("stale-session")
	t.Cleanup(ClearSession)

	var requests, reauthCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	oldReauthenticate := reauthenticateSession
	reauthenticateSession = func(context.Context) error {
		reauthCalls++
		SetSession("fresh-session")
		return nil
	}
	t.Cleanup(func() { reauthenticateSession = oldReauthenticate })

	client, err := NewAPIClient(&Config{ServerURL: srv.URL}, "stale-session", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	var out bwListResponse[bwFolder]
	err = client.getJSON(context.Background(), srv.URL, &out)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("getJSON() = %v, want HTTP 401", err)
	}
	if requests != 2 || reauthCalls != 1 {
		t.Fatalf("requests=%d reauthCalls=%d, want 2/1", requests, reauthCalls)
	}
	if HasSession() {
		t.Fatalf("第二次 401 后 session 应被清除，got %q", Session())
	}
}

// 全局 UserKey 是进程级状态，设置它的用例不能并行。
func TestAPIClient_PushBundle_CreateSecureNotePayload(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{{ID: "f1", Name: "vikunja"}},
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

	noteRel := "env/vikunja.env"
	content := "VIKUNJA_API_TOKEN=abc\n"
	result, err := client.PushBundle(context.Background(), PushBundleRequest{
		Target: declaredPTarget(t, "vikunja", SyncPlaneProject),
	}, []SecureNote{{RelativePath: noteRel, Content: content}})
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
	// folder 只有项目名一级，平面与相对路径都编码进条目名。
	wantName := projectItemName(t, noteRel)
	if gotName != wantName {
		t.Fatalf("解密 name = %q, want %q", gotName, wantName)
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
	noteRel := "env/vikunja.env"
	encName, err := encryptVaultString(projectItemName(t, noteRel), itemKey)
	if err != nil {
		t.Fatal(err)
	}
	encNotes, err := encryptVaultString("VIKUNJA_API_TOKEN=old\n", itemKey)
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
				Data: []bwFolder{{ID: "f1", Name: "vikunja"}},
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

	target := declaredPTarget(t, "vikunja", SyncPlaneProject)
	newContent := "VIKUNJA_API_TOKEN=new\n"
	result, err := client.PushBundle(context.Background(), PushBundleRequest{
		Target: target,
	}, []SecureNote{{RelativePath: noteRel, Content: newContent}})
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

	pullResult, err := client.PullBundle(context.Background(), PullBundleRequest{Target: target})
	if err != nil {
		t.Fatalf("PullBundle() after update = %v", err)
	}
	if len(pullResult.Notes) != 1 {
		t.Fatalf("Notes = %#v", pullResult.Notes)
	}
	// push 只改正文，远端条目名保持原样——否则迁移期间会把改好的消费者路径名改回去。
	if pullResult.Notes[0].RelativePath != noteRel {
		t.Fatalf("RelativePath = %q, want %q（push 不应改远端条目名）", pullResult.Notes[0].RelativePath, noteRel)
	}
	if gotName, _ := updateBody["name"].(string); gotName != encName {
		t.Fatalf("update 应原样回传远端 name 密文: got %q want %q", gotName, encName)
	}
	if pullResult.Notes[0].Content != newContent {
		t.Fatalf("Content = %q, want %q", pullResult.Notes[0].Content, newContent)
	}
}

// 缺 folder 且未开 CreateFolderIfMissing：push 必须报错，不静默建 folder。
func TestAPIClient_PushBundle_MissingFolderErrorsWithoutFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/api/folders":
			if r.Method == http.MethodPost {
				t.Fatal("未开 CreateFolderIfMissing 不应 POST /folders")
			}
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{Data: nil})
		case "/api/ciphers":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{Data: nil})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewAPIClient(&Config{ServerURL: srv.URL}, "sess-nofolder", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	SetUserKey(bytes.Repeat([]byte{0x05}, 64))
	t.Cleanup(ClearSession)

	_, err = client.PushBundle(context.Background(), PushBundleRequest{
		Target: declaredPTarget(t, "cnb", SyncPlaneProject),
	}, []SecureNote{{RelativePath: ".gcm/cnb.yaml", Content: "x"}})
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("缺 folder 且未开 flag 应报 folder 不存在, got %v", err)
	}
}

// 开 CreateFolderIfMissing：folder 不存在时先 POST /folders 建 folder，再落 note。
func TestAPIClient_PushBundle_CreatesFolderWhenMissing(t *testing.T) {
	userKey := bytes.Repeat([]byte{0x06}, 64)
	var (
		folderPosts int
		folderName  string
		cipherPosts int
		created     bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch {
		case r.URL.Path == "/api/folders" && r.Method == http.MethodPost:
			folderPosts++
			var body map[string]any
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &body)
			enc, _ := body["name"].(string)
			if !looksEncrypted(enc) {
				t.Fatalf("folder name 应加密: %q", enc)
			}
			if name, err := decryptVaultString(enc, userKey); err == nil {
				folderName = name
			}
			created = true
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/folders" && r.Method == http.MethodGet:
			if created {
				_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{Data: []bwFolder{{ID: "f-new", Name: enc(t, "cnb", userKey)}}})
			} else {
				_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{Data: nil})
			}
		case r.URL.Path == "/api/ciphers" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{Data: nil})
		case r.URL.Path == "/api/ciphers" && r.Method == http.MethodPost:
			cipherPosts++
			var body map[string]any
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &body)
			if body["folderId"] != "f-new" {
				t.Fatalf("note 应落到新建 folder f-new, got %v", body["folderId"])
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewAPIClient(&Config{ServerURL: srv.URL}, "sess-mkfolder", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	SetUserKey(userKey)
	t.Cleanup(ClearSession)

	result, err := client.PushBundle(context.Background(), PushBundleRequest{
		Target:                declaredPTarget(t, "cnb", SyncPlaneProject),
		CreateFolderIfMissing: true,
	}, []SecureNote{{RelativePath: ".gcm/cnb.yaml", Content: "host: cnb.cool\n"}})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	// 新建的 folder 名只有项目名一级，不含 private/<plane>。
	if folderPosts != 1 || folderName != "cnb" {
		t.Fatalf("应恰好建一次 folder cnb, posts=%d name=%q", folderPosts, folderName)
	}
	if cipherPosts != 1 || result.Created != 1 {
		t.Fatalf("应在新 folder 建 1 条 note, cipherPosts=%d result=%#v", cipherPosts, result)
	}
}

// enc 是测试辅助：用给定 key 加密明文，模拟 Bitwarden folder name 密文。
func enc(t *testing.T, plain string, key []byte) string {
	t.Helper()
	s, err := encryptVaultString(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// 条目名只有一种合法形态（private/<plane>/<同步根相对路径>），匹配按精确名走。
func TestAPIClient_PushBundle_UpdateMatchesNameExactly(t *testing.T) {
	var updatedID string
	var updateBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch {
		case r.URL.Path == "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{{ID: "f1", Name: "vikunja"}},
			})
		case r.URL.Path == "/api/ciphers" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{
				Data: []bwCipher{{
					ID:       "cipher-legacy",
					Type:     cipherTypeSecureNote,
					Name:     projectItemName(t, "env/vikunja.env"),
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
		Target: declaredPTarget(t, "vikunja", SyncPlaneProject),
	}, []SecureNote{{RelativePath: "env/vikunja.env", Content: "VIKUNJA_API_TOKEN=new\n"}})
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

func TestAPIClient_PullBundle_DecryptsSSHKey(t *testing.T) {
	userKey := bytes.Repeat([]byte{0x07}, 64)
	itemKey, err := generateCipherKey()
	if err != nil {
		t.Fatal(err)
	}
	encItemKey, err := encryptVaultBytes(itemKey, userKey)
	if err != nil {
		t.Fatal(err)
	}
	mustEncItem := func(plain string) string {
		t.Helper()
		out, encErr := encryptVaultString(plain, itemKey)
		if encErr != nil {
			t.Fatal(encErr)
		}
		return out
	}
	encFolder, err := encryptVaultString("vikunja", userKey)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch r.URL.Path {
		case "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{{ID: "f1", Name: encFolder}},
			})
		case "/api/ciphers":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{
				Data: []bwCipher{
					{
						ID: "note-1", Type: cipherTypeSecureNote, FolderID: "f1", Key: encItemKey,
						Name:  mustEncItem(projectItemName(t, "env/vikunja.env")),
						Notes: mustEncItem("VIKUNJA_API_TOKEN=abc\n"),
					},
					{
						ID: "ssh-1", Type: cipherTypeSSHKey, FolderID: "f1", Key: encItemKey,
						Name:  mustEncItem(projectItemName(t, "deploy")),
						Notes: mustEncItem("vikunja.example.com\n"),
						SSHKey: &bwSSHKey{
							PrivateKey:     mustEncItem("-----BEGIN OPENSSH PRIVATE KEY-----\nSECRET\n-----END OPENSSH PRIVATE KEY-----\n"),
							PublicKey:      mustEncItem("ssh-ed25519 AAAA deploy\n"),
							KeyFingerprint: mustEncItem("SHA256:abc"),
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{ServerURL: srv.URL}
	client, err := NewAPIClient(cfg, "sess-ssh", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	SetUserKey(userKey)
	t.Cleanup(ClearSession)

	target := declaredPTarget(t, "vikunja", SyncPlaneProject)
	result, err := client.PullBundle(context.Background(), PullBundleRequest{Target: target})
	if err != nil {
		t.Fatalf("PullBundle() = %v", err)
	}
	if len(result.Notes) != 1 {
		t.Fatalf("Notes = %#v", result.Notes)
	}
	if len(result.SSHKeys) != 1 {
		t.Fatalf("SSHKeys = %#v", result.SSHKeys)
	}
	key := result.SSHKeys[0]
	if key.Name != "deploy" || len(key.Hosts) != 1 || key.Hosts[0] != "vikunja.example.com" {
		t.Fatalf("SSHKey meta = %#v", key)
	}
	if !strings.Contains(key.PrivateKey, "SECRET") {
		t.Fatal("私钥应解密成功")
	}
	if key.KeyFingerprint != "SHA256:abc" {
		t.Fatalf("fingerprint = %q", key.KeyFingerprint)
	}

	listed, err := client.ListSSHKeys(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Name != "deploy" {
		t.Fatalf("ListSSHKeys = %#v", listed)
	}
}

// Bitwarden 按 EncryptedString 校验 notes，空字符串会被判非法密文；没登记 hosts
// 的 SSH Key（notes 为 null）重命名时必须回传 null 而不是 ""。
func TestAPIClient_RenameSSHKey_EmptyNotesStaysNull(t *testing.T) {
	userKey := bytes.Repeat([]byte{0x0b}, 64)
	itemKey, err := generateCipherKey()
	if err != nil {
		t.Fatal(err)
	}
	encItemKey, err := encryptVaultBytes(itemKey, userKey)
	if err != nil {
		t.Fatal(err)
	}
	encName, err := encryptVaultString(projectItemName(t, "tencent_cvm"), itemKey)
	if err != nil {
		t.Fatal(err)
	}
	encFolder, err := encryptVaultString("tencent-cloud", userKey)
	if err != nil {
		t.Fatal(err)
	}

	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch {
		case r.URL.Path == "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{{ID: "f1", Name: encFolder}},
			})
		case r.URL.Path == "/api/ciphers" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{
				Data: []bwCipher{{
					ID: "ssh-1", Type: cipherTypeSSHKey, FolderID: "f1", Key: encItemKey,
					Name: encName, SSHKey: &bwSSHKey{},
				}},
			})
		case r.URL.Path == "/api/ciphers/ssh-1" && r.Method == http.MethodPut:
			raw, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if unmarshalErr := json.Unmarshal(raw, &putBody); unmarshalErr != nil {
				t.Fatal(unmarshalErr)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewAPIClient(&Config{ServerURL: srv.URL}, "sess-rename", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	SetUserKey(userKey)
	t.Cleanup(ClearSession)

	if err := client.RenameSSHKey(context.Background(), RenameSSHKeyRequest{
		OldName: "tencent_cvm",
		NewName: ".sshkey/tencent_cvm",
		Target:  declaredPTarget(t, "tencent-cloud", SyncPlaneProject),
	}); err != nil {
		t.Fatalf("RenameSSHKey() = %v", err)
	}
	if putBody == nil {
		t.Fatal("未收到 PUT /ciphers/ssh-1")
	}
	if notes, ok := putBody["notes"]; !ok || notes != nil {
		t.Fatalf("notes = %#v，应为 null", putBody["notes"])
	}
}

// Bitwarden 只有整库 /folders 与 /ciphers 列表接口，浏览 N 个地址不能变成 2N 次全库下载。
func TestAPIClient_ListNotes_ReusesVaultSnapshot(t *testing.T) {
	userKey := bytes.Repeat([]byte{0x09}, 64)
	itemKey, err := generateCipherKey()
	if err != nil {
		t.Fatal(err)
	}
	encItemKey, err := encryptVaultBytes(itemKey, userKey)
	if err != nil {
		t.Fatal(err)
	}
	mustEncItem := func(plain string) string {
		t.Helper()
		out, encErr := encryptVaultString(plain, itemKey)
		if encErr != nil {
			t.Fatal(encErr)
		}
		return out
	}
	mustEncUser := func(plain string) string {
		t.Helper()
		out, encErr := encryptVaultString(plain, userKey)
		if encErr != nil {
			t.Fatal(encErr)
		}
		return out
	}

	var folderCalls, cipherCalls, deleteCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBitwardenHeaders(t, r)
		switch {
		case r.URL.Path == "/api/folders":
			folderCalls++
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{
					{ID: "f1", Name: mustEncUser("one")},
					{ID: "f2", Name: mustEncUser("two")},
				},
			})
		case r.URL.Path == "/api/ciphers" && r.Method == http.MethodGet:
			cipherCalls++
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{
				Data: []bwCipher{
					{ID: "n1", Type: cipherTypeSecureNote, FolderID: "f1", Key: encItemKey,
						Name: mustEncItem(projectItemName(t, "env/one.env")), Notes: mustEncItem("A=1\n")},
					{ID: "n2", Type: cipherTypeSecureNote, FolderID: "f2", Key: encItemKey,
						Name: mustEncItem(projectItemName(t, "env/two.env")), Notes: mustEncItem("B=2\n")},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/api/ciphers/") && r.Method == http.MethodDelete:
			deleteCalls++
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewAPIClient(&Config{ServerURL: srv.URL}, "sess-cache", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	SetUserKey(userKey)
	t.Cleanup(ClearSession)

	ctx := context.Background()
	targets := []SyncTarget{
		declaredPTarget(t, "one", SyncPlaneProject),
		declaredPTarget(t, "two", SyncPlaneProject),
	}
	for _, target := range targets {
		notes, listErr := client.ListNotes(ctx, target)
		if listErr != nil {
			t.Fatalf("ListNotes(%s) = %v", target.Address, listErr)
		}
		if len(notes) != 1 {
			t.Fatalf("ListNotes(%s) = %#v", target.Address, notes)
		}
		if _, listErr = client.ListSSHKeys(ctx, target); listErr != nil {
			t.Fatalf("ListSSHKeys(%s) = %v", target.Address, listErr)
		}
	}
	if _, err = client.ListPNames(ctx); err != nil {
		t.Fatalf("ListPNames() = %v", err)
	}
	if folderCalls != 1 || cipherCalls != 1 {
		t.Fatalf("folders=%d ciphers=%d, 期望各 1 次全库下载", folderCalls, cipherCalls)
	}

	// 写操作后必须重新取快照，否则删除完还会读到旧 cipher。
	if err = client.DeleteSecureNote(ctx, DeleteSecureNoteRequest{
		Target:   targets[0],
		NotePath: "env/one.env",
	}); err != nil {
		t.Fatalf("DeleteSecureNote() = %v", err)
	}
	if deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", deleteCalls)
	}
	if _, err = client.ListNotes(ctx, targets[0]); err != nil {
		t.Fatalf("ListNotes() = %v", err)
	}
	if folderCalls != 2 || cipherCalls != 2 {
		t.Fatalf("写操作后 folders=%d ciphers=%d, 期望快照失效后各重取 1 次", folderCalls, cipherCalls)
	}
}
