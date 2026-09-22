package assets

import (
	"strings"
	"testing"
)

func TestGlobalAssetsIncludeBuiltinSkills(t *testing.T) {
	bundle := GlobalAssets()
	if len(bundle.Skills) != 2 {
		t.Fatalf("期望 2 个内置 skills，得到 %d", len(bundle.Skills))
	}

	want := map[string]bool{
		"dec":               false,
		"dec-extract-asset": false,
	}
	for _, skill := range bundle.Skills {
		seen := false
		for _, file := range skill.Files {
			if file.RelPath == "SKILL.md" {
				seen = true
				break
			}
		}
		if _, ok := want[skill.Name]; ok {
			want[skill.Name] = seen
		}
	}

	for name, ok := range want {
		if !ok {
			t.Fatalf("内置 skill %s 缺少 SKILL.md 或未注册", name)
		}
	}
}

func TestBuiltinDecSkillUsesConsoleMCPSurface(t *testing.T) {
	bundle := GlobalAssets()
	var body string
	for _, skill := range bundle.Skills {
		if skill.Name != "dec" {
			continue
		}
		for _, file := range skill.Files {
			if file.RelPath == "SKILL.md" {
				body = string(file.Content)
			}
		}
	}
	if body == "" {
		t.Fatal("缺少内置 dec/SKILL.md")
	}
	for _, banned := range []string{"`dec list`", "`dec search`", "`dec config init`", "`dec config global`", "`dec push --remove`", "`dec pull`"} {
		if strings.Contains(body, banned) {
			t.Fatalf("内置 dec skill 仍含已下线 CLI %q", banned)
		}
	}
	for _, want := range []string{"dec_pull", "dec_list_assets", "Console"} {
		if !strings.Contains(body, want) {
			t.Fatalf("内置 dec skill 应包含 %q", want)
		}
	}
}

func TestBuiltinExtractAssetSkillGatesPersonalWrites(t *testing.T) {
	bundle := GlobalAssets()
	var body string
	for _, skill := range bundle.Skills {
		if skill.Name != "dec-extract-asset" {
			continue
		}
		for _, file := range skill.Files {
			if file.RelPath == "SKILL.md" {
				body = string(file.Content)
			}
		}
	}
	if body == "" {
		t.Fatal("缺少内置 dec-extract-asset/SKILL.md")
	}
	for _, banned := range []string{"`vault=cli`"} {
		if strings.Contains(body, banned) {
			t.Fatalf("dec-extract-asset 仍含过时提问模板 %q", banned)
		}
	}
	for _, want := range []string{
		"硬门禁",
		"~/.cursor/skills",
		"dec_propose_upstream",
		"缺提供方时绝不能用「先写个人 Skill」顶替",
		"覆写已写 + 上游票据已开",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("dec-extract-asset 应包含 %q", want)
		}
	}
}

func TestGlobalAssetsReturnsCopies(t *testing.T) {
	bundle := GlobalAssets()
	if len(bundle.Skills) == 0 || len(bundle.Skills[0].Files) == 0 {
		t.Fatal("内置 skills 不应为空")
	}

	originalSkillName := bundle.Skills[0].Name
	originalFirstByte := bundle.Skills[0].Files[0].Content[0]
	bundle.Skills[0].Name = "mutated"
	bundle.Skills[0].Files[0].Content[0] = 'X'

	fresh := GlobalAssets()
	if fresh.Skills[0].Name != originalSkillName {
		t.Fatalf("GlobalAssets 应返回副本，期望名称 %s，得到 %s", originalSkillName, fresh.Skills[0].Name)
	}
	if fresh.Skills[0].Files[0].Content[0] != originalFirstByte {
		t.Fatal("GlobalAssets 应返回独立文件内容副本")
	}
}
