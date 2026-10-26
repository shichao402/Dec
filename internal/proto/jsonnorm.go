package proto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// JSON 条目的内容规范化。
//
// 这个文件存在的理由是一次真实的长期漂移：MCP 条目走 json_key 格式下发，
// 服务端把「条目在源文件里的原始字节」当作哈希，代理把「容器内托管条目
// 紧凑序列化后的字节」当作哈希。源文件是两行缩进的
// `{\n  "url": "..."\n}`，落盘后是 `{"url":"..."}`，两边永不可能相等，
// 于是每一轮对账都判定为漂移：设备列表长期显示 drift=1，同一条 MCP
// 被反复重发。
//
// 两类错误都很隐蔽：写错位置（ADR 0050）至少还能人眼看见条目没了；
// 哈希口径不一致完全没有外部症状，只有那个永远不为零的数字。
//
// 结论：json_key 条目的哈希必须算「规范化后的语义」，两侧走同一个函数，
// 且不依赖任何一侧的落盘格式。

// ErrNotJSON 表示内容不是单个合法 JSON 值。
var ErrNotJSON = errors.New("不是单个合法 JSON")

// NormalizeJSONEntry 把一段 JSON 条目规范化成稳定字节。
//
// 规范化口径：
//
//   - 解析后重新紧凑序列化，吸收空白、换行、缩进与键序差异。
//   - 数字按 json.Number 原样保留，不经过 float64，否则大整数
//     （如毫秒时间戳）会被改写成科学计数法，于是「内容没变但哈希变了」。
//
// 解析失败时返回错误而不是退回原始字节：调用方需要知道内容不可用，
// 静默退回会让「内容坏掉」和「内容没变」无法区分。
func NormalizeJSONEntry(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotJSON, err)
	}
	// 解码后还有残留说明是多段 JSON，语义上不是一个条目。
	if dec.More() {
		return nil, fmt.Errorf("%w: 含多段 JSON", ErrNotJSON)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: 重新序列化失败: %v", ErrNotJSON, err)
	}
	return out, nil
}
