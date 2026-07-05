package secrets

import (
	"encoding/base64"
	"testing"
)

func TestMasterPasswordHash_MatchesBitwardenPBKDF2Vector(t *testing.T) {
	t.Parallel()

	// bitwarden_crypto keys/kdf.rs test_master_key_derive_pbkdf2
	password := "67t9b5g67$%Dh89n"
	email := "test_key"
	iterations := 10_000
	wantMasterKey := []byte{
		31, 79, 104, 226, 150, 71, 177, 90, 194, 80, 172, 209, 17, 129, 132, 81,
		138, 167, 69, 167, 254, 149, 2, 27, 39, 197, 64, 42, 22, 195, 86, 75,
	}
	salt := []byte(email)
	gotMasterKey := pbkdf2SHA256([]byte(password), salt, iterations, 32)
	if string(gotMasterKey) != string(wantMasterKey) {
		t.Fatalf("master key = %q, want %q", base64.StdEncoding.EncodeToString(gotMasterKey), base64.StdEncoding.EncodeToString(wantMasterKey))
	}

	got := masterPasswordHash(password, email, iterations)
	want := base64.StdEncoding.EncodeToString(pbkdf2SHA256(gotMasterKey, []byte(password), 1, 32))
	if got != want {
		t.Fatalf("masterPasswordHash() = %q, want %q", got, want)
	}

	// 旧实现误用 "account.key" 作 salt，结果必须不同。
	wrong := base64.StdEncoding.EncodeToString(pbkdf2SHA256(gotMasterKey, []byte("account.key"), 1, 32))
	if got == wrong {
		t.Fatalf("server hash 不应再使用 account.key salt")
	}
}
