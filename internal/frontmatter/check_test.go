package frontmatter

import (
	"errors"
	"testing"
)

func TestCheckDescription(t *testing.T) {
	cases := []struct {
		name string
		in   string
		bad  bool
	}{
		{"单行", "---\nname: demo\ndescription: 做一件事。用于某场景。\n---\n", false},
		{"单行带引号", "---\ndescription: \"含: 冒号\"\nname: x\n---\n", false},
		{"单行在最后一个键", "---\nname: demo\ndescription: d\n---\n", false},
		{"后面跟列表键", "---\ndescription: d\nglobs:\n  - '*'\n---\n", false},
		{"没有元数据块", "# 只有正文\n", false},
		{"没有 description", "---\nname: demo\n---\n", false},
		{"YAML 写坏了", "---\ndescription: : :\n  - x\n---\n", false},
		{"折叠块", "---\nname: demo\ndescription: >\n  第一行\n  第二行\n---\n", true},
		{"折叠块去尾", "---\ndescription: >-\n  a\n---\n", true},
		{"字面块", "---\ndescription: |\n  a\n  b\n---\n", true},
		{"普通标量跨行", "---\ndescription: 第一行\n  第二行\nname: x\n---\n", true},
		{"引号跨行", "---\ndescription: \"第一行\n  第二行\"\n---\n", true},
		{"CRLF 折叠块", "---\r\ndescription: >\r\n  a\r\n---\r\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckDescription([]byte(c.in))
			if c.bad != errors.Is(err, ErrMultilineDescription) {
				t.Fatalf("CheckDescription() = %v，期望拒绝=%v", err, c.bad)
			}
		})
	}
}
