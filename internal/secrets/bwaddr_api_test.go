package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAPIClient_PushP_UsesFlatFolderAndPrefixedItemName 锁死扁平布局：folder 只有
// 项目名，平面进条目名。
func TestAPIClient_PushP_UsesFlatFolderAndPrefixedItemName(t *testing.T) {
	userKey := bytes.Repeat([]byte{0x07}, 64)
	SetUserKey(userKey)
	t.Cleanup(ClearSession)

	encFolderName, err := encryptVaultString("dec", userKey)
	if err != nil {
		t.Fatal(err)
	}

	var (
		createdFolder map[string]any
		createdCipher map[string]any
		folderExists  bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/folders" && r.Method == http.MethodGet:
			data := []bwFolder{}
			if folderExists {
				data = append(data, bwFolder{ID: "p1", Name: encFolderName})
			}
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{Data: data})
		case r.URL.Path == "/api/folders" && r.Method == http.MethodPost:
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				http.Error(w, readErr.Error(), http.StatusBadRequest)
				return
			}
			if err := json.Unmarshal(body, &createdFolder); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			folderExists = true
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/ciphers" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{})
		case r.URL.Path == "/api/ciphers" && r.Method == http.MethodPost:
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				http.Error(w, readErr.Error(), http.StatusBadRequest)
				return
			}
			if err := json.Unmarshal(body, &createdCipher); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewAPIClient(&Config{ServerURL: srv.URL}, "sess-p", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewPSyncTarget("dec", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.PushBundle(context.Background(), PushBundleRequest{
		Target:                target,
		CreateFolderIfMissing: true,
	}, []SecureNote{{RelativePath: ".env/dec.env", Content: "TOKEN=abc\n"}})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("result = %#v", result)
	}

	if createdFolder == nil {
		t.Fatal("未创建 folder")
	}
	folderName, _ := createdFolder["name"].(string)
	gotFolder, err := decryptVaultString(folderName, userKey)
	if err != nil {
		t.Fatal(err)
	}
	if gotFolder != "dec" {
		t.Fatalf("folder 名 = %q，Bitwarden folder 只能是项目名这一级", gotFolder)
	}

	if createdCipher == nil {
		t.Fatal("未创建 cipher")
	}
	cipherKey, _ := createdCipher["key"].(string)
	itemKey, err := itemDecryptionKey(cipherKey, userKey)
	if err != nil {
		t.Fatal(err)
	}
	encName, _ := createdCipher["name"].(string)
	gotName, err := decryptVaultString(encName, itemKey)
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "private/local/.env/dec.env" {
		t.Fatalf("条目名 = %q，平面必须编码进条目名", gotName)
	}
}

// TestAPIClient_ListP_SeparatesPlanesInSameFolder 确认同一个项目 folder 里两个平面
// 的条目互不串台，且列出的地址是逻辑地址。
func TestAPIClient_ListP_SeparatesPlanesInSameFolder(t *testing.T) {
	userKey := bytes.Repeat([]byte{0x09}, 64)
	SetUserKey(userKey)
	t.Cleanup(ClearSession)

	encFolderName, err := encryptVaultString("dec", userKey)
	if err != nil {
		t.Fatal(err)
	}
	newCipher := func(id, itemName string) bwCipher {
		itemKey, keyErr := generateCipherKey()
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		encItemKey, keyErr := encryptVaultBytes(itemKey, userKey)
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		encName, keyErr := encryptVaultString(itemName, itemKey)
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		encNotes, keyErr := encryptVaultString("TOKEN=x\n", itemKey)
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		return bwCipher{
			ID: id, Type: cipherTypeSecureNote, Name: encName,
			Notes: encNotes, FolderID: "p1", Key: encItemKey,
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/folders":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwFolder]{
				Data: []bwFolder{{ID: "p1", Name: encFolderName}},
			})
		case "/api/ciphers":
			_ = json.NewEncoder(w).Encode(bwListResponse[bwCipher]{Data: []bwCipher{
				newCipher("c1", "private/project/.env/proj.env"),
				newCipher("c2", "private/user/.env/user.env"),
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewAPIClient(&Config{ServerURL: srv.URL}, "sess-list", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	addresses, err := client.ListAddresses(ctx)
	if err != nil {
		t.Fatalf("ListAddresses() = %v", err)
	}
	want := map[string]bool{"dec/private/local": false, "dec/private/global": false}
	for _, addr := range addresses {
		if _, ok := want[addr]; !ok {
			t.Fatalf("多出地址 %q（addresses=%v）", addr, addresses)
		}
		want[addr] = true
	}
	for addr, seen := range want {
		if !seen {
			t.Fatalf("缺少地址 %q（addresses=%v）", addr, addresses)
		}
	}

	projectNotes, err := client.ListNotes(ctx, declaredPTarget(t, "dec", SyncPlaneProject))
	if err != nil {
		t.Fatalf("ListNotes(project) = %v", err)
	}
	if len(projectNotes) != 1 || projectNotes[0].Name != ".env/proj.env" {
		t.Fatalf("project notes = %#v", projectNotes)
	}
	userNotes, err := client.ListNotes(ctx, declaredPTarget(t, "dec", SyncPlaneMachine))
	if err != nil {
		t.Fatalf("ListNotes(user) = %v", err)
	}
	if len(userNotes) != 1 || userNotes[0].Name != ".env/user.env" {
		t.Fatalf("user notes = %#v", userNotes)
	}
}
