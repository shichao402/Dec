package assets

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

type FileAsset struct {
	RelPath string
	Content []byte
}

type SkillAsset struct {
	Name  string
	Files []FileAsset
}

type RuleAsset struct {
	Name    string
	Content []byte
}

type MCPAsset struct {
	Name    string
	Content []byte
}

type Bundle struct {
	Skills []SkillAsset
	Rules  []RuleAsset
	MCPs   []MCPAsset
}

//go:embed dec dec-extract-asset
var builtinFS embed.FS

var globalAssets = Bundle{
	Skills: []SkillAsset{
		mustLoadSkillAsset("dec"),
		mustLoadSkillAsset("dec-extract-asset"),
	},
}

func GlobalAssets() Bundle {
	return Bundle{
		Skills: cloneSkillAssets(globalAssets.Skills),
		Rules:  cloneRuleAssets(globalAssets.Rules),
		MCPs:   cloneMCPAssets(globalAssets.MCPs),
	}
}

func mustLoadSkillAsset(dir string) SkillAsset {
	files := make([]FileAsset, 0, 1)
	foundSkillMD := false

	err := fs.WalkDir(builtinFS, dir, func(entryPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		content, err := builtinFS.ReadFile(entryPath)
		if err != nil {
			return err
		}

		relPath := strings.TrimPrefix(entryPath, dir+"/")
		if relPath == "SKILL.md" {
			foundSkillMD = true
		}
		files = append(files, FileAsset{RelPath: relPath, Content: content})
		return nil
	})
	if err != nil {
		panic(fmt.Sprintf("加载内置 skill %s 失败: %v", dir, err))
	}
	if !foundSkillMD {
		panic(fmt.Sprintf("内置 skill %s 缺少 SKILL.md", dir))
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})

	return SkillAsset{Name: path.Base(dir), Files: files}
}

func cloneSkillAssets(in []SkillAsset) []SkillAsset {
	out := make([]SkillAsset, 0, len(in))
	for _, skill := range in {
		clonedFiles := make([]FileAsset, len(skill.Files))
		for i, file := range skill.Files {
			clonedFiles[i] = FileAsset{RelPath: file.RelPath, Content: append([]byte(nil), file.Content...)}
		}
		out = append(out, SkillAsset{Name: skill.Name, Files: clonedFiles})
	}
	return out
}

func cloneRuleAssets(in []RuleAsset) []RuleAsset {
	out := make([]RuleAsset, len(in))
	for i, rule := range in {
		out[i] = RuleAsset{Name: rule.Name, Content: append([]byte(nil), rule.Content...)}
	}
	return out
}

func cloneMCPAssets(in []MCPAsset) []MCPAsset {
	out := make([]MCPAsset, len(in))
	for i, mcp := range in {
		out[i] = MCPAsset{Name: mcp.Name, Content: append([]byte(nil), mcp.Content...)}
	}
	return out
}
