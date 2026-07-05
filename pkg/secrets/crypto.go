package secrets

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := func(key, data []byte) []byte {
		h := hmac.New(sha256.New, key)
		h.Write(data)
		return h.Sum(nil)
	}
	hashLen := sha256.Size
	numBlocks := (keyLen + hashLen - 1) / hashLen
	buf := make([]byte, numBlocks*hashLen)
	for block := 1; block <= numBlocks; block++ {
		idx := []byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)}
		u := prf(password, append(salt, idx...))
		t := append([]byte(nil), u...)
		for i := 1; i < iter; i++ {
			u = prf(password, u)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		copy(buf[(block-1)*hashLen:], t)
	}
	return buf[:keyLen]
}

// masterPasswordHash 生成 Bitwarden Identity 登录用的 server auth hash。
// 1) PBKDF2-SHA256(password, lower(trim(email)), kdfIterations) → master key
// 2) PBKDF2-SHA256(masterKey, password, 1) → 发送给 /connect/token 的 password 字段
func masterPasswordHash(password, email string, iterations int) string {
	salt := []byte(strings.ToLower(strings.TrimSpace(email)))
	key := pbkdf2SHA256([]byte(password), salt, iterations, 32)
	hash := pbkdf2SHA256(key, []byte(password), 1, 32)
	return base64.StdEncoding.EncodeToString(hash)
}
