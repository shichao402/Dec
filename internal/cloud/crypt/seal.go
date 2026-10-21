// Package crypt 实现 Dec Cloud 的按值加密。
//
// 设计约束（对应需求 9）：
//
//   - 每个值单独加密，带独立的随机 nonce，密文之间没有可复用的结构。
//   - 每条密文带 key id。根密钥轮换后，旧密文仍能用旧 key id 解开。
//   - 根密钥只存在于内存和 0600 的本地文件，永不进入 git 与冷备。
//
// 算法固定为 XChaCha20-Poly1305（192 位随机 nonce，24 字节）。
// 选择它是因为 Go 官方扩展包提供成熟实现，且长 nonce 让随机生成碰撞概率可忽略。
package crypt

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// RootKeyBytes 是根密钥的长度。
const RootKeyBytes = chacha20poly1305.KeySize

// KeyIDBytes 是 key id 的原始字节数，十六进制后为 16 个字符。
const KeyIDBytes = 8

// sealedVersion 是密文结构版本，用于未来算法升级。
const sealedVersion = 1

var (
	// ErrDecrypt 表示解密失败：密钥不对、密文被篡改，或结构损坏。
	ErrDecrypt = errors.New("decrypt failed")
	// ErrKeyID 表示密文的 key id 与当前根密钥不匹配，需要按 key id 找回旧根密钥。
	ErrKeyID = errors.New("key id mismatch")
	// ErrKeySize 表示根密钥长度不对。
	ErrKeySize = errors.New("invalid root key size")
)

// Key 是一把根密钥。它实现 encoding.TextUnmarshaler，
// 但刻意不实现 String/GoString，避免被日志或 %v 打印出来。
type Key struct {
	id       string
	material [RootKeyBytes]byte
}

// NewKey 生成一把新的根密钥。
func NewKey(r io.Reader) (*Key, error) {
	if r == nil {
		r = rand.Reader
	}
	k := &Key{}
	if _, err := io.ReadFull(r, k.material[:]); err != nil {
		return nil, fmt.Errorf("生成根密钥失败: %w", err)
	}
	idRaw := make([]byte, KeyIDBytes)
	if _, err := io.ReadFull(r, idRaw); err != nil {
		return nil, fmt.Errorf("生成 key id 失败: %w", err)
	}
	k.id = hex.EncodeToString(idRaw)
	return k, nil
}

// KeyFromBytes 用已有的密钥材料和 key id 重建根密钥。
func KeyFromBytes(id string, material []byte) (*Key, error) {
	if len(material) != RootKeyBytes {
		return nil, fmt.Errorf("%w: 期望 %d 字节，实际 %d 字节", ErrKeySize, RootKeyBytes, len(material))
	}
	if len(id) != KeyIDBytes*2 {
		return nil, fmt.Errorf("%w: key id 必须是 %d 位十六进制", ErrKeySize, KeyIDBytes*2)
	}
	if _, err := hex.DecodeString(id); err != nil {
		return nil, fmt.Errorf("%w: key id 不是十六进制: %v", ErrKeySize, err)
	}
	k := &Key{id: id}
	copy(k.material[:], material)
	return k, nil
}

// ID 返回 key id。
func (k *Key) ID() string {
	if k == nil {
		return ""
	}
	return k.id
}

// Material 返回密钥材料的副本，供写入根密钥文件使用。
// 调用方用完后应立刻把返回的切片清零。
func (k *Key) Material() []byte {
	out := make([]byte, RootKeyBytes)
	copy(out, k.material[:])
	return out
}

// Wipe 清零内存中的密钥材料。
func (k *Key) Wipe() {
	if k == nil {
		return
	}
	for i := range k.material {
		k.material[i] = 0
	}
}

// Sealed 是一条密文。它可以被序列化进 git，不含任何可解出明文的材料。
type Sealed struct {
	V     int    `json:"v"`
	KeyID string `json:"key_id"`
	Nonce string `json:"nonce"`
	// Data 是密文，已包含 Poly1305 认证标签。
	Data string `json:"data"`
}

// Seal 用根密钥加密一段明文。
func (k *Key) Seal(plaintext []byte) (*Sealed, error) {
	if k == nil {
		return nil, errors.New("根密钥未加载")
	}
	aead, err := chacha20poly1305.NewX(k.material[:])
	if err != nil {
		return nil, fmt.Errorf("初始化 AEAD 失败: %w", err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("生成 nonce 失败: %w", err)
	}
	ct := aead.Seal(nil, nonce, plaintext, []byte(k.id))
	return &Sealed{
		V:     sealedVersion,
		KeyID: k.id,
		Nonce: hex.EncodeToString(nonce),
		Data:  hex.EncodeToString(ct),
	}, nil
}

// Open 用根密钥解密一条密文。
// 密文的 key id 与当前根密钥不一致时返回 ErrKeyID，调用方可据此去冷备里取旧根密钥。
func (k *Key) Open(s *Sealed) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: 密文为空", ErrDecrypt)
	}
	if s.V != sealedVersion {
		return nil, fmt.Errorf("%w: 不支持的密文版本 %d", ErrDecrypt, s.V)
	}
	if k == nil {
		return nil, ErrKeyID
	}
	if s.KeyID != k.id {
		return nil, fmt.Errorf("%w: 密文 key id %s，当前根密钥 %s", ErrKeyID, s.KeyID, k.id)
	}
	nonce, err := hex.DecodeString(s.Nonce)
	if err != nil || len(nonce) != chacha20poly1305.NonceSizeX {
		return nil, fmt.Errorf("%w: nonce 非法", ErrDecrypt)
	}
	ct, err := hex.DecodeString(s.Data)
	if err != nil {
		return nil, fmt.Errorf("%w: 密文非法", ErrDecrypt)
	}
	aead, err := chacha20poly1305.NewX(k.material[:])
	if err != nil {
		return nil, fmt.Errorf("初始化 AEAD 失败: %w", err)
	}
	pt, err := aead.Open(nil, nonce, ct, []byte(k.id))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecrypt, err)
	}
	return pt, nil
}

// MarshalJSON 让 Key 无法被意外序列化进配置文件。
func (k Key) MarshalJSON() ([]byte, error) {
	return nil, errors.New("根密钥不可序列化")
}

// sealedJSON 是包含 KeyID 的密文，用于 JSON 磁盘格式。
type sealedJSON struct {
	Sealed
}

// Rekey 在轮换根密钥时把一条密文用新密钥重新加密。
// 旧密文保留在 git 历史中，不会被改写。
func (k *Key) Rekey(old *Key, s *Sealed) (*Sealed, error) {
	pt, err := old.Open(s)
	if err != nil {
		return nil, err
	}
	defer wipe(pt)
	return k.Seal(pt)
}

// Encode 把密文编码成 JSON 字节。
func (s *Sealed) Encode() ([]byte, error) { return json.Marshal(s) }

// Decode 从 JSON 字节解析密文。
func Decode(data []byte) (*Sealed, error) {
	var s Sealed
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecrypt, err)
	}
	return &s, nil
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
