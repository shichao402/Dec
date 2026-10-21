package ssot

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
)

// 哈希统一走 SHA-256，输出 "sha256:<hex>" 形式。
//
// 代理对账时用的是同一套算法：它当场计算本地落点的哈希上报，
// 服务端与自己渲染出的内容比对。因为代理不在本地保存下发清单（需求 5.2），
// 这个哈希是唯一的对账依据，算法必须双端一致，所以放在协议层的共享包里。

// Prefix 是哈希前缀。
const Prefix = "sha256:"

func newHasher() hash.Hash { return sha256.New() }

// SumHex 计算内容哈希。
func SumHex(data []byte) string {
	sum := sha256.Sum256(data)
	return Prefix + hex.EncodeToString(sum[:])
}
