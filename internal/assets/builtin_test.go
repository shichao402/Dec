package assets

import "testing"

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
