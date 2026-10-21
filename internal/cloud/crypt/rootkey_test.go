package crypt

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRootKeyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "root.key")

	original := newTestKey(t)
	created := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	if err := WriteRootKey(path, original, created, ""); err != nil {
		t.Fatalf("WriteRootKey: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != FileMode {
		t.Fatalf("权限应为 %o，实际 %o", FileMode, perm)
	}

	loaded, err := LoadRootKey(path)
	if err != nil {
		t.Fatalf("LoadRootKey: %v", err)
	}
	if loaded.ID() != original.ID() {
		t.Fatalf("key id 不一致: %s != %s", loaded.ID(), original.ID())
	}

	sealed, err := original.Seal([]byte("value"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := loaded.Open(sealed); err != nil {
		t.Fatalf("加载后的根密钥无法解密: %v", err)
	}
}

func TestLoadRootKeyMissing(t *testing.T) {
	_, err := LoadRootKey(filepath.Join(t.TempDir(), "absent.key"))
	if !errors.Is(err, ErrRootKeyMissing) {
		t.Fatalf("期望 ErrRootKeyMissing，实际 %v", err)
	}
}

func TestLoadRootKeyCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "root.key")

	cases := map[string]string{
		"非 JSON":    "not json at all",
		"版本不对":      `{"v":99,"key_id":"0011223344556677","material":"00"}`,
		"材料不足":      `{"v":1,"key_id":"0011223344556677","material":"00"}`,
		"key id 非法": `{"v":1,"key_id":"zz","material":"0000000000000000000000000000000000000000000000000000000000000000"}`,
	}
	for name, content := range cases {
		if err := os.WriteFile(path, []byte(content), FileMode); err != nil {
			t.Fatalf("%s: 写入失败: %v", name, err)
		}
		if _, err := LoadRootKey(path); !errors.Is(err, ErrRootKeyCorrupt) {
			t.Fatalf("%s: 期望 ErrRootKeyCorrupt，实际 %v", name, err)
		}
	}
}

func TestLoadRootKeyTightensPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "root.key")
	key := newTestKey(t)
	if err := WriteRootKey(path, key, time.Time{}, ""); err != nil {
		t.Fatalf("WriteRootKey: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	if _, err := LoadRootKey(path); err != nil {
		t.Fatalf("LoadRootKey: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != FileMode {
		t.Fatalf("加载后权限应被收紧为 %o，实际 %o", FileMode, perm)
	}
}

func TestRootKeyPayloadRoundTrip(t *testing.T) {
	key := newTestKey(t)
	created := time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

	payload, err := RootKeyPayload(key, created, "prevkey0000000000")
	if err != nil {
		t.Fatalf("RootKeyPayload: %v", err)
	}

	restored, gotCreated, rotatedFrom, err := ParseRootKeyPayload(payload)
	if err != nil {
		t.Fatalf("ParseRootKeyPayload: %v", err)
	}
	if restored.ID() != key.ID() {
		t.Fatalf("key id 不一致: %s != %s", restored.ID(), key.ID())
	}
	if !gotCreated.Equal(created) {
		t.Fatalf("创建时间不一致: %v != %v", gotCreated, created)
	}
	if rotatedFrom != "prevkey0000000000" {
		t.Fatalf("轮换来源不一致: %q", rotatedFrom)
	}

	// 冷备内容与本地文件内容必须逐字节一致，读回比对才有意义。
	path := filepath.Join(t.TempDir(), "root.key")
	if err := WriteRootKey(path, key, created, "prevkey0000000000"); err != nil {
		t.Fatalf("WriteRootKey: %v", err)
	}
	fileData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(fileData, payload) {
		t.Fatalf("冷备内容与本地文件不一致:\nfile=%s\npayload=%s", fileData, payload)
	}
}

func TestWriteRootKeyCreatesDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "root.key")
	key := newTestKey(t)
	if err := WriteRootKey(path, key, time.Time{}, ""); err != nil {
		t.Fatalf("WriteRootKey: %v", err)
	}
	if _, err := LoadRootKey(path); err != nil {
		t.Fatalf("LoadRootKey: %v", err)
	}
}
