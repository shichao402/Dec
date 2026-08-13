package secrets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
)

const encTypeAesCbc256Hmac = "2"

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

// deriveStretchedKeys 从主密码派生 Bitwarden 用于解密用户 symmetric key 的 enc/mac 子钥。
func deriveStretchedKeys(password, salt string, iterations int) (encKey, macKey []byte) {
	tempKey := pbkdf2SHA256([]byte(password), []byte(salt), iterations, 32)
	return hkdfExpand(tempKey, []byte("enc"), 32), hkdfExpand(tempKey, []byte("mac"), 32)
}

// decryptUserKey 解密 /accounts/profile 返回的 key 字段，得到 64 字节 vault symmetric key。
func decryptUserKey(encryptedKey, password, salt string, iterations int) ([]byte, error) {
	encKey, macKey := deriveStretchedKeys(password, salt, iterations)
	plain, err := decryptEncBytes(encryptedKey, encKey, macKey)
	if err != nil {
		return nil, fmt.Errorf("解密 Bitwarden user key 失败: %w", err)
	}
	if len(plain) != 64 {
		return nil, fmt.Errorf("Bitwarden user key 长度异常: %d", len(plain))
	}
	return plain, nil
}

func decryptVaultString(encrypted string, vaultKey []byte) (string, error) {
	plain, err := decryptVaultBytes(encrypted, vaultKey)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func encryptVaultString(plain string, vaultKey []byte) (string, error) {
	encrypted, err := encryptVaultBytes([]byte(plain), vaultKey)
	if err != nil {
		return "", err
	}
	return encrypted, nil
}

func encryptVaultBytes(plain, vaultKey []byte) (string, error) {
	if len(vaultKey) != 64 {
		return "", fmt.Errorf("vault symmetric key 未就绪")
	}
	return encryptEncBytes(plain, vaultKey[:32], vaultKey[32:])
}

// generateCipherKey 生成 Bitwarden cipher 专用 64 字节对称密钥。
func generateCipherKey() ([]byte, error) {
	key := make([]byte, 64)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成 cipher key 失败: %w", err)
	}
	return key, nil
}

func decryptVaultBytes(encrypted string, vaultKey []byte) ([]byte, error) {
	if !looksEncrypted(encrypted) {
		return []byte(encrypted), nil
	}
	if len(vaultKey) != 64 {
		return nil, fmt.Errorf("vault symmetric key 未就绪")
	}
	return decryptEncBytes(encrypted, vaultKey[:32], vaultKey[32:])
}

func looksEncrypted(s string) bool {
	dot := strings.IndexByte(s, '.')
	if dot <= 0 || dot > 2 {
		return false
	}
	for i := 0; i < dot; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return strings.Count(s[dot:], "|") >= 2
}

func hkdfExpand(prk, info []byte, length int) []byte {
	hashLen := sha256.Size
	n := (length + hashLen - 1) / hashLen
	okm := make([]byte, 0, n*hashLen)
	var t []byte
	for i := 1; i <= n; i++ {
		mac := hmac.New(sha256.New, prk)
		if len(t) > 0 {
			mac.Write(t)
		}
		mac.Write(info)
		mac.Write([]byte{byte(i)})
		t = mac.Sum(nil)
		okm = append(okm, t...)
	}
	return okm[:length]
}

func decryptEncBytes(encrypted string, encKey, macKey []byte) ([]byte, error) {
	version, payload, ok := strings.Cut(encrypted, ".")
	if !ok || version != encTypeAesCbc256Hmac {
		return nil, fmt.Errorf("不支持的加密类型")
	}
	fields := strings.Split(payload, "|")
	if len(fields) != 3 {
		return nil, fmt.Errorf("加密 payload 格式无效")
	}
	iv, err := base64.StdEncoding.DecodeString(fields[0])
	if err != nil {
		return nil, fmt.Errorf("解码 IV 失败: %w", err)
	}
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("IV 长度无效")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return nil, fmt.Errorf("解码 ciphertext 失败: %w", err)
	}
	mac, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil {
		return nil, fmt.Errorf("解码 MAC 失败: %w", err)
	}
	h := hmac.New(sha256.New, macKey)
	h.Write(iv)
	h.Write(ciphertext)
	if subtle.ConstantTimeCompare(h.Sum(nil), mac) != 1 {
		return nil, fmt.Errorf("MAC 校验失败")
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext 块对齐无效")
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(ciphertext))
	mode.CryptBlocks(plain, ciphertext)
	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

func encryptEncBytes(plain, encKey, macKey []byte) (string, error) {
	if len(plain) == 0 {
		return "", fmt.Errorf("plain empty")
	}
	padded := pkcs7Pad(plain, aes.BlockSize)
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("生成 IV 失败: %w", err)
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return "", err
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	h := hmac.New(sha256.New, macKey)
	h.Write(iv)
	h.Write(ciphertext)
	mac := h.Sum(nil)
	return fmt.Sprintf("%s.%s|%s|%s",
		encTypeAesCbc256Hmac,
		base64.StdEncoding.EncodeToString(iv),
		base64.StdEncoding.EncodeToString(ciphertext),
		base64.StdEncoding.EncodeToString(mac),
	), nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padLen := blockSize - (len(data) % blockSize)
	if padLen == 0 {
		padLen = blockSize
	}
	padding := bytes.Repeat([]byte{byte(padLen)}, padLen)
	return append(data, padding...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("padding 无效")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > blockSize || padLen > len(data) {
		return nil, fmt.Errorf("padding 无效")
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if data[i] != byte(padLen) {
			return nil, fmt.Errorf("padding 无效")
		}
	}
	return data[:len(data)-padLen], nil
}
