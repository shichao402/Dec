// Package frontmatter 读取 Markdown 文件开头的 YAML 元数据块。
//
// Agent Skills 规范（agentskills.io/specification）要求 SKILL.md 以元数据块开头，
// 其中 description 必填，用来说明 skill 做什么、什么时候用；Cursor 的 .mdc 规则
// 也用同样的块声明 description。资产的描述只有这一份来源，服务端展示时
// 直接从正文里读，不在 manifest 里另存，免得两处说法不一致。
package frontmatter

import (
	"bytes"
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxScan 限制只在文件开头这么多字节里找元数据块，防止超大文件拖慢列表。
const maxScan = 64 << 10

// Description 返回元数据块里的 description，没有块或没有该字段时返回空串。
//
// 折行与多余空白折成单个空格，便于在表格里一行展示。解析失败视为没有描述：
// 描述只是展示信息，不应因为某个文件写得不规范就让整个读取失败。
func Description(content []byte) string {
	block, ok := extract(content)
	if !ok {
		return ""
	}
	var meta struct {
		Description any `yaml:"description"`
	}
	if err := yaml.Unmarshal(block, &meta); err != nil {
		return ""
	}
	s, ok := meta.Description.(string)
	if !ok {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

// ErrMultilineDescription 表示 description 没有写在 "description:" 同一行。
var ErrMultilineDescription = errors.New("description 必须写在同一行")

// CheckDescription 要求 description 写在 "description:" 同一行。
//
// YAML 的 ">"、"|" 块写法和跨行续写都是合法 YAML，Agent Skills 规范也允许，
// 但有宿主（With）只认单行写法，遇到块写法就读不出描述，skill 列表里显示
// 为空。Dec 的资产要下发给所有宿主，所以写入时统一收紧到单行。
// 太长或含 ": " 等特殊字符时用双引号包起来，仍写在一行。
//
// 没有元数据块、没有 description 或 YAML 本身写坏时不在这里报错：
// 前两种不是格式问题，最后一种交给读取方按“没有描述”处理。
func CheckDescription(content []byte) error {
	block, ok := extract(content)
	if !ok {
		return nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(block, &doc); err != nil {
		return nil
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := m.Content[i], m.Content[i+1]
		if key.Value != "description" {
			continue
		}
		if val.Kind != yaml.ScalarNode {
			return nil
		}
		if val.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || val.Line != key.Line {
			return ErrMultilineDescription
		}
		// 普通或带引号的标量也能跨行续写：下一行缩进即属于这个值。
		lines := strings.Split(strings.ReplaceAll(string(block), "\r\n", "\n"), "\n")
		if next := key.Line; next < len(lines) {
			if l := lines[next]; strings.TrimSpace(l) != "" && (l[0] == ' ' || l[0] == '\t') {
				return ErrMultilineDescription
			}
		}
		return nil
	}
	return nil
}

// extract 取出文件开头 "---" 行与下一处 "---" 或 "..." 行之间的内容。
func extract(content []byte) ([]byte, bool) {
	if len(content) > maxScan {
		content = content[:maxScan]
	}
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	lines := bytes.SplitAfter(content, []byte("\n"))
	if len(lines) == 0 || string(bytes.TrimRight(lines[0], "\r\n")) != "---" {
		return nil, false
	}
	var b bytes.Buffer
	for _, l := range lines[1:] {
		switch string(bytes.TrimRight(l, "\r\n")) {
		case "---", "...":
			return b.Bytes(), true
		}
		b.Write(l)
	}
	return nil, false
}
