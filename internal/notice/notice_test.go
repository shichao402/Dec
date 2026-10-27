package notice

import (
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/proto"
)

// 说明行必须写明固化工具与参数，并且保留 Text 作为识别标记：
// 卸载扫描与幂等判断都按「包含 Text」认领，Text 一变旧落点就不再被认。
func TestLineCarriesLocator(t *testing.T) {
	got := Line(Locator{Bundle: "relkit", Path: "skills/relkit-ops/SKILL.md"})
	for _, want := range []string{Text, EditTool, "bundle=relkit", "path=skills/relkit-ops/SKILL.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("说明行缺少 %q：%s", want, got)
		}
	}
	if !strings.HasPrefix(got, Text) {
		t.Fatalf("Text 必须在最前面：%s", got)
	}
}

// 定位信息缺失或可能打断注释时退回只有 Text 的形式，而不是写出半截指令。
func TestLineFallsBack(t *testing.T) {
	for _, loc := range []Locator{{}, {Bundle: "a"}, {Path: "p"}, {Bundle: "a", Path: "x-->y"}, {Bundle: "a", Path: "x\ny"}} {
		if got := Line(loc); got != Text {
			t.Fatalf("%+v 应退回 Text，实际 %q", loc, got)
		}
	}
}

func TestApplyWritesLocatedLineAfterFrontMatter(t *testing.T) {
	loc := Locator{Bundle: "relkit", Path: "skills/relkit-ops/SKILL.md"}
	in := "---\nname: relkit-ops\n---\n# 正文\n"
	got := Apply("skills/relkit-ops/SKILL.md", in, false, proto.FormatWhole, loc)
	want := "---\nname: relkit-ops\n---\n<!-- " + Line(loc) + " -->\n# 正文\n"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	// 幂等：已经带说明（包括旧版只有 Text 的）不再重复加。
	if again := Apply("x.md", got, false, proto.FormatWhole, loc); again != got {
		t.Fatalf("重复 Apply 不应改变内容")
	}
	old := "<!-- " + Text + " -->\n# 正文\n"
	if again := Apply("x.md", old, false, proto.FormatWhole, loc); again != old {
		t.Fatalf("旧版说明行不应再叠加一行")
	}
}

// YAML 用 # 注释，含定位的说明行也不能让文件变成非法 YAML（单行、无换行）。
func TestApplyYAMLSingleLine(t *testing.T) {
	got := Apply("a.yaml", "k: v\n", false, proto.FormatWhole, Locator{Bundle: "b", Path: "a.yaml"})
	first := strings.SplitN(got, "\n", 2)[0]
	if !strings.HasPrefix(first, "# ") || !strings.Contains(first, "bundle=b") {
		t.Fatalf("YAML 说明行错误：%q", got)
	}
}
