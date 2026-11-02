package frontmatter

import "testing"

func TestDescription(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"标准 SKILL.md", "---\nname: demo\ndescription: 做一件事。用于某场景。\n---\n# 正文\n", "做一件事。用于某场景。"},
		{"折行写法", "---\nname: demo\ndescription: >\n  第一行\n  第二行\n---\nbody\n", "第一行 第二行"},
		{"CRLF 与 BOM", "\xef\xbb\xbf---\r\ndescription: d\r\n---\r\nbody\r\n", "d"},
		{"Cursor 规则", "---\ndescription: 风格\nglobs: ['*']\n---\nalways be terse\n", "风格"},
		{"说明行在块之后", "---\ndescription: d\n---\n<!-- 由 Dec 管理 -->\n# x\n", "d"},
		{"没有元数据块", "# 只有正文\n", ""},
		{"块没有闭合", "---\ndescription: d\n# x\n", ""},
		{"没有 description", "---\nname: demo\n---\n", ""},
		{"description 不是字符串", "---\ndescription: [a, b]\n---\n", ""},
		{"YAML 写坏了", "---\ndescription: : :\n  - x\n---\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Description([]byte(c.in)); got != c.want {
				t.Fatalf("Description() = %q，期望 %q", got, c.want)
			}
		})
	}
}
