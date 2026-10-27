package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/notice"
)

// TestScanManagedKeepsUserFiles 锁住兜底扫描的边界：只认带 Dec 说明行的文件。
//
// 这是一次实测发现的真实缺陷。卸载时 ScanManaged 会把类别目录下的文件
// 一律当成 Dec 写的删掉，而 skills/ 与 rules/ 是用户和 Dec 共用的目录：
// 用户自己的 skill、自己写的规则躺在同一层，卸载就一起没了。
// 卸载不可逆，误删没有任何补救余地，所以这条必须先失败过再修好。
func TestScanManagedKeepsUserFiles(t *testing.T) {
	dir := t.TempDir()

	// Dec 写的：带说明行。
	ours := filepath.Join(dir, "dec-demo.md")
	writeFile(t, ours, "<!-- "+notice.Text+" -->\n# 内容\n")

	// 用户自己的：同目录、同扩展名，只是没有说明行。
	userRule := filepath.Join(dir, "my-personal-rule.mdc")
	writeFile(t, userRule, "# 我自己的规则\n")

	userSkill := filepath.Join(dir, "my-own-skill")
	if err := os.MkdirAll(userSkill, 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	writeFile(t, filepath.Join(userSkill, "SKILL.md"), "# 我自己的 skill\n")

	// front matter 之后带说明行的也算 Dec 的（Cursor 规则就是这么落的）。
	frontMatter := filepath.Join(dir, "cursor-style.mdc")
	writeFile(t, frontMatter, "---\ndescription: d\n---\n<!-- "+notice.Text+" -->\n# 内容\n")

	// 新版说明行带定位信息（ADR 0053），卸载扫描同样要认领它。
	located := filepath.Join(dir, "located.md")
	writeFile(t, located, "<!-- "+notice.Line(notice.Locator{Bundle: "woa", Path: "skills/x/SKILL.md"})+" -->\n# 内容\n")

	got, err := ScanManaged(dir)
	if err != nil {
		t.Fatalf("ScanManaged: %v", err)
	}

	want := map[string]bool{ours: true, frontMatter: true, located: true}
	if len(got) != len(want) {
		t.Fatalf("只应扫出 Dec 写的 %d 个文件，实际 %d 个: %v", len(want), len(got), got)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("扫出了不属于 Dec 的文件: %s", p)
		}
	}
}

// TestScanManagedIgnoresMissingDir 验证目录不存在时不报错。
//
// 卸载要能在没装过任何宿主的机器上跑完，扫描不存在目录不能算失败。
func TestScanManagedIgnoresMissingDir(t *testing.T) {
	got, err := ScanManaged(filepath.Join(t.TempDir(), "不存在"))
	if err != nil {
		t.Fatalf("目录不存在不该报错: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("目录不存在应返回空，实际 %v", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写 %s: %v", path, err)
	}
}
