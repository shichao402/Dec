package crypt

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func newTestKey(t *testing.T) *Key {
	t.Helper()
	k, err := NewKey(nil)
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	return k
}

func TestSealOpenRoundTrip(t *testing.T) {
	k := newTestKey(t)
	plain := []byte("sk-live-abcdef0123456789")

	s, err := k.Seal(plain)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if s.KeyID != k.ID() {
		t.Fatalf("key id 不一致: %s != %s", s.KeyID, k.ID())
	}
	if bytes.Contains([]byte(s.Data), plain) {
		t.Fatal("密文中出现了明文")
	}

	got, err := k.Open(s)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("明文不一致: %q", got)
	}
}

func TestSealIsRandomized(t *testing.T) {
	k := newTestKey(t)
	plain := []byte("same-value")

	a, err := k.Seal(plain)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	b, err := k.Seal(plain)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if a.Nonce == b.Nonce || a.Data == b.Data {
		t.Fatal("两次加密同一明文得到了相同结果，nonce 未随机化")
	}
}

func TestOpenWithWrongKeyID(t *testing.T) {
	k1 := newTestKey(t)
	k2 := newTestKey(t)
	s, err := k1.Seal([]byte("value"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := k2.Open(s); !errors.Is(err, ErrKeyID) {
		t.Fatalf("期望 ErrKeyID，实际 %v", err)
	}
}

func TestOpenTampered(t *testing.T) {
	k := newTestKey(t)
	s, err := k.Seal([]byte("value"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// 翻转最后一个字节，认证标签必须失败。
	b := []byte(s.Data)
	if b[len(b)-1] == 'a' {
		b[len(b)-1] = 'b'
	} else {
		b[len(b)-1] = 'a'
	}
	s.Data = string(b)
	if _, err := k.Open(s); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("期望 ErrDecrypt，实际 %v", err)
	}
}

func TestKeyFromBytes(t *testing.T) {
	k := newTestKey(t)
	material := k.Material()
	defer func() {
		for i := range material {
			material[i] = 0
		}
	}()

	restored, err := KeyFromBytes(k.ID(), material)
	if err != nil {
		t.Fatalf("KeyFromBytes: %v", err)
	}
	s, err := k.Seal([]byte("value"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := restored.Open(s); err != nil {
		t.Fatalf("还原后的根密钥无法解密: %v", err)
	}

	if _, err := KeyFromBytes(k.ID(), material[:10]); !errors.Is(err, ErrKeySize) {
		t.Fatalf("期望 ErrKeySize，实际 %v", err)
	}
	if _, err := KeyFromBytes("not-hex!!", material); !errors.Is(err, ErrKeySize) {
		t.Fatalf("期望 ErrKeySize，实际 %v", err)
	}
}

func TestRekey(t *testing.T) {
	oldKey := newTestKey(t)
	newKey := newTestKey(t)
	plain := []byte("rotate-me")

	s, err := oldKey.Seal(plain)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	rotated, err := newKey.Rekey(oldKey, s)
	if err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	if rotated.KeyID != newKey.ID() {
		t.Fatalf("轮换后 key id 未更新: %s", rotated.KeyID)
	}
	got, err := newKey.Open(rotated)
	if err != nil {
		t.Fatalf("Open 轮换后的密文: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("轮换后明文不一致: %q", got)
	}
	// 旧密文在新密钥下必须报 ErrKeyID，而不是静默失败。
	if _, err := newKey.Open(s); !errors.Is(err, ErrKeyID) {
		t.Fatalf("期望 ErrKeyID，实际 %v", err)
	}
}

func TestWipe(t *testing.T) {
	k := newTestKey(t)
	s, err := k.Seal([]byte("x"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	k.Wipe()
	if _, err := k.Open(s); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("清零后不应能解开原密文，实际 %v", err)
	}
}

func TestSealedJSONStable(t *testing.T) {
	k := newTestKey(t)
	s, err := k.Seal([]byte("value"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	data, err := s.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, err := k.Open(back); err != nil {
		t.Fatalf("解码后无法解密: %v", err)
	}
	if _, err := Decode([]byte("not json")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("期望 ErrDecrypt，实际 %v", err)
	}
}

func TestKeyNotSerializable(t *testing.T) {
	k := newTestKey(t)
	if _, err := json.Marshal(k); err == nil {
		t.Fatal("根密钥不应能被 JSON 序列化")
	}
}
