// Package release 是发布清单的签名与校验（ADR 0058）。
//
// 自动更新让「服务端给什么就装什么」成为可能，所以不能只靠服务端自己算的
// sha256：那只防传输损坏，防不了服务端被攻破。这里用 ed25519 给发布清单
// 签名，私钥只在 GitHub 的仓库 Secret 里，公钥编译进代理。服务器上没有
// 私钥，攻破服务器也伪造不出代理肯装的版本。
//
// 签名对象是 release.json 的原始字节（不是解析后再序列化的结果），校验通过后
// 清单里每个文件的 sha256 才可信，再由 sha256 把信任传递到二进制本身。
package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// TrustedKeys 是代理信任的发布签名公钥（base64，32 字节）。
//
// 写成列表是为了轮换：换钥匙时先发一个同时信任新旧两把公钥的版本，
// 等设备都升上去后再用新私钥签名、去掉旧公钥。只放一把的话，换钥匙
// 那一刻所有旧设备都会拒绝新版本，只能逐台手动重装。
//
// 私钥：GitHub 仓库 Secret RELEASE_SIGNING_KEY（base64 的 32 字节种子）。
var TrustedKeys = []string{
	// 2026-10-29 生成。
	"arBdhjYMuC+V5Rmq7JhrAIK4WdLvY9L5tYK6WgvFDUQ=",
}

// ManifestName 与 SignatureName 是下载通道里清单与签名的文件名。
const (
	ManifestName  = "release.json"
	SignatureName = "release.json.sig"
)

// Manifest 是发布清单中自动更新关心的部分。
type Manifest struct {
	Version        string `json:"version"`
	RequiredClient string `json:"required_client"`
	Protocol       int    `json:"protocol"`
	BuiltAt        string `json:"built_at"`
	Files          []File `json:"files"`
}

// File 是一个发布产物。
type File struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// ErrBadSignature 表示签名与所有信任公钥都对不上。
var ErrBadSignature = errors.New("发布清单签名校验失败")

// Sign 用 base64 编码的 ed25519 种子对数据签名，返回 base64 签名文本。
func Sign(seedB64 string, data []byte) (string, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(seedB64))
	if err != nil {
		return "", fmt.Errorf("签名私钥不是合法的 base64: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return "", fmt.Errorf("签名私钥长度应为 %d 字节，实际 %d", ed25519.SeedSize, len(seed))
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
	return base64.StdEncoding.EncodeToString(sig) + "\n", nil
}

// Verify 用信任公钥列表校验签名，任一把对上即通过。
func Verify(keys []string, data []byte, sigText string) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigText))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: 签名格式不对", ErrBadSignature)
	}
	for _, k := range keys {
		pub, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
			return nil
		}
	}
	return ErrBadSignature
}

// ParseVerified 先验签再解析：未经校验的字节不进入任何后续逻辑。
func ParseVerified(keys []string, data []byte, sigText string) (*Manifest, error) {
	if err := Verify(keys, data, sigText); err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("发布清单解析失败: %w", err)
	}
	if _, ok := ParseVersion(m.Version); !ok {
		return nil, fmt.Errorf("发布清单版本号非法: %q", m.Version)
	}
	return &m, nil
}

// Find 返回指定名字的产物。
func (m *Manifest) Find(name string) (File, bool) {
	for _, f := range m.Files {
		if f.Name == name {
			return f, true
		}
	}
	return File{}, false
}

// CompareVersion 比较 v1.2.3 形式的版本号：-1 表示 a 旧，0 相同，1 表示 a 新。
// 任一方无法解析时返回 0，ok 为 false。
func CompareVersion(a, b string) (int, bool) {
	pa, oka := ParseVersion(a)
	pb, okb := ParseVersion(b)
	if !oka || !okb {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		switch {
		case pa[i] < pb[i]:
			return -1, true
		case pa[i] > pb[i]:
			return 1, true
		}
	}
	return 0, true
}

// ParseVersion 解析严格的 vX.Y.Z（也接受不带 v 的写法）。
//
// 比服务端的解析更严：不接受 -rc 之类后缀。自动更新只认正式版本号，
// 本地构建的 dev 版本解析失败，于是天然不会被自动更新覆盖。
func ParseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		if p == "" {
			return out, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return out, false
			}
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
