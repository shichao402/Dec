// Package frontmatter 读取 Markdown 文件开头的 YAML 元数据块。
//
// Agent Skills 规范（agentskills.io/specification）要求 SKILL.md 以元数据块开头，
// 其中 description 必填，用来说明 skill 做什么、什么时候用；Cursor 的 .mdc 规则
// 也用同样的块声明 description。资产的描述只有这一份来源，服务端展示时
// 直接从正文里读，不在 manifest 里另存，免得两处说法不一致。
package frontmatter

import (
	"bytes"
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
