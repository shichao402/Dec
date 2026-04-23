package bundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

func TestParseMember(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    types.BundleMember
		wantErr bool
	}{
		{name: "skill 单数", in: "skill/vikunja-workflow", want: types.BundleMember{Type: "skill", Name: "vikunja-workflow"}},
		{name: "skills 复数", in: "skills/vikunja-workflow", want: types.BundleMember{Type: "skill", Name: "vikunja-workflow"}},
		{name: "rule 单数", in: "rule/vikunja-integration", want: types.BundleMember{Type: "rule", Name: "vikunja-integration"}},
		{name: "rules 复数", in: "rules/vikunja-integration", want: types.BundleMember{Type: "rule", Name: "vikunja-integration"}},
		{name: "mcp 单数", in: "mcp/vikunja-mcp", want: types.BundleMember{Type: "mcp", Name: "vikunja-mcp"}},
		{name: "mcps 复数", in: "mcps/vikunja-mcp", want: types.BundleMember{Type: "mcp", Name: "vikunja-mcp"}},
		{name: "允许首尾空白", in: "  skills/foo  ", want: types.BundleMember{Type: "skill", Name: "foo"}},
		{name: "空串", in: "", wantErr: true},
		{name: "缺少斜杠", in: "skills-vikunja", wantErr: true},
		{name: "缺少名字", in: "skills/", wantErr: true},
		{name: "缺少类型", in: "/vikunja", wantErr: true},
		{name: "非法类型", in: "bundle/vikunja", wantErr: true},
		{name: "未知类型", in: "foo/bar", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseMember(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望错误，但成功返回 %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLoadBundles_Empty(t *testing.T) {
	vault := t.TempDir()
	// 目录里完全没有 bundles/
	bundles, warnings, err := LoadBundles(vault, nil)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(bundles) != 0 {
		t.Fatalf("无 bundles 目录时应返回空列表, got %d", len(bundles))
	}
	if len(warnings) != 0 {
		t.Fatalf("无 bundles 目录时不应产生 warning, got %d", len(warnings))
	}
}

func TestLoadBundles_BundlesDirExistsButEmpty(t *testing.T) {
	vault := t.TempDir()
	if err := os.Mkdir(filepath.Join(vault, BundlesDirName), 0755); err != nil {
		t.Fatal(err)
	}
	bundles, warnings, err := LoadBundles(vault, nil)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(bundles) != 0 {
		t.Fatalf("空 bundles 目录应返回空列表, got %d", len(bundles))
	}
	if len(warnings) != 0 {
		t.Fatalf("空 bundles 目录不应产生 warning, got %d", len(warnings))
	}
}

func TestLoadBundles_HappyPath(t *testing.T) {
	vault := t.TempDir()
	writeBundle(t, vault, "vikunja.yaml", `
name: vikunja
description: Vikunja 工作流
members:
  - mcp/vikunja-mcp
  - rules/vikunja-integration
  - skills/vikunja-workflow
`)
	writeBundle(t, vault, "helloworld.yaml", `
name: helloworld
members:
  - skill/helloworld
`)

	bundles, warnings, err := LoadBundles(vault, nil)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("正常场景不应有 warning: %+v", warnings)
	}
	if len(bundles) != 2 {
		t.Fatalf("期望 2 个 bundle，得到 %d", len(bundles))
	}
	// 按 name 升序
	if bundles[0].Name != "helloworld" || bundles[1].Name != "vikunja" {
		t.Fatalf("bundle 排序不正确: %v", bundles)
	}
	if bundles[1].Description != "Vikunja 工作流" {
		t.Fatalf("description 解析错误: %q", bundles[1].Description)
	}
	if len(bundles[1].Members) != 3 {
		t.Fatalf("members 长度错误: %d", len(bundles[1].Members))
	}
}

func TestLoadBundles_DuplicateNameIsFatal(t *testing.T) {
	vault := t.TempDir()
	writeBundle(t, vault, "a.yaml", `
name: dup
members:
  - skill/foo
`)
	writeBundle(t, vault, "b.yaml", `
name: dup
members:
  - skill/bar
`)
	_, _, err := LoadBundles(vault, nil)
	if err == nil {
		t.Fatalf("重名 bundle 应该致命报错")
	}
}

func TestLoadBundles_InvalidMemberReferenceIsFatal(t *testing.T) {
	vault := t.TempDir()
	writeBundle(t, vault, "x.yaml", `
name: x
members:
  - bundle/nope
`)
	_, _, err := LoadBundles(vault, nil)
	if err == nil {
		t.Fatalf("非法成员引用应致命报错")
	}
}

func TestLoadBundles_MissingNameIsFatal(t *testing.T) {
	vault := t.TempDir()
	writeBundle(t, vault, "x.yaml", `
members:
  - skill/foo
`)
	_, _, err := LoadBundles(vault, nil)
	if err == nil {
		t.Fatalf("缺 name 应致命报错")
	}
}

func TestLoadBundles_EmptyMembersIsFatal(t *testing.T) {
	vault := t.TempDir()
	writeBundle(t, vault, "x.yaml", `
name: x
members: []
`)
	_, _, err := LoadBundles(vault, nil)
	if err == nil {
		t.Fatalf("空 members 应致命报错")
	}
}

func TestLoadBundles_IllegalNameIsFatal(t *testing.T) {
	vault := t.TempDir()
	writeBundle(t, vault, "x.yaml", `
name: "-illegal"
members:
  - skill/foo
`)
	_, _, err := LoadBundles(vault, nil)
	if err == nil {
		t.Fatalf("首字符为 - 的 name 应致命报错")
	}
}

func TestLoadBundles_NonYAMLFileProducesWarning(t *testing.T) {
	vault := t.TempDir()
	writeBundle(t, vault, "README.md", "not yaml")
	writeBundle(t, vault, "ok.yaml", `
name: ok
members:
  - skill/foo
`)
	bundles, warnings, err := LoadBundles(vault, nil)
	if err != nil {
		t.Fatalf("非 yaml 文件应只产生 warning: %v", err)
	}
	if len(bundles) != 1 {
		t.Fatalf("应解析出 1 个 bundle, got %d", len(bundles))
	}
	if len(warnings) != 1 {
		t.Fatalf("应产生 1 条 warning, got %d", len(warnings))
	}
}

func TestLoadBundles_MemberExistsWarning(t *testing.T) {
	vault := t.TempDir()
	writeBundle(t, vault, "x.yaml", `
name: x
members:
  - skill/present
  - skill/missing
`)
	exists := func(m types.BundleMember) bool {
		return m.Name == "present"
	}
	bundles, warnings, err := LoadBundles(vault, exists)
	if err != nil {
		t.Fatalf("成员存在性检查失败不应致命: %v", err)
	}
	if len(bundles) != 1 {
		t.Fatalf("期望 1 个 bundle, got %d", len(bundles))
	}
	if len(warnings) != 1 {
		t.Fatalf("期望 1 条 missing member warning, got %d (%+v)", len(warnings), warnings)
	}
	if warnings[0].BundleName != "x" {
		t.Fatalf("warning 未归属到正确 bundle: %+v", warnings[0])
	}
}

func TestLoadBundles_InvalidYAMLIsFatal(t *testing.T) {
	vault := t.TempDir()
	writeBundle(t, vault, "bad.yaml", "::: not-yaml :::")
	_, _, err := LoadBundles(vault, nil)
	if err == nil {
		t.Fatalf("非法 YAML 应致命报错")
	}
}

// writeBundle 是测试辅助：把内容写入 <vault>/bundles/<name>。
// 自动创建 bundles 目录。
func writeBundle(t *testing.T, vault, name, content string) {
	t.Helper()
	dir := filepath.Join(vault, BundlesDirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
