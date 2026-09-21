package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/ide"
	"github.com/shichao402/Dec/internal/types"
)

// 提供方下架一个 Skill 后，安装会整目录重写 cache，资产就此消失。
// 若不在安装前记下渲染过什么，消费方的 .cursor/skills 里会永远留着那一份。
func TestPruneRemovedOfficialAssetsDropsIDECopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	project := t.TempDir()
	cursor := ide.Get("cursor")
	if cursor == nil {
		t.Fatal("cursor IDE 未注册")
	}
	workspace := NewWorkspace(WorkspaceProject, project)
	req := types.RequiresSpec{"playbook": types.RequiresLatest}

	cacheSkill := filepath.Join(project, ".dec", "cache", "playbook", "public", "local", "skills", "dropped")
	if err := os.MkdirAll(cacheSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheSkill, "SKILL.md"), []byte("# dropped\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := officialCacheInventory(workspace, req)
	if len(before) != 1 {
		t.Fatalf("安装前清单 = %#v", before)
	}

	projectIDEs := []ide.IDE{cursor}
	result := &PullProjectAssetsResult{}
	if err := renderOfficialFromCache(workspace, req, projectIDEs, result, nil); err != nil {
		t.Fatal(err)
	}
	rendered := filepath.Join(cursor.SkillsDirForPlane(workspace.IDEPlane(), project, home), managedName("dropped"))
	if _, err := os.Stat(rendered); err != nil {
		t.Fatalf("渲染失败，测试前提不成立: %v", err)
	}

	// 模拟一次安装：上游删了这个资产，cache 被整目录重写。
	if err := os.RemoveAll(filepath.Join(project, ".dec", "cache", "playbook")); err != nil {
		t.Fatal(err)
	}

	pruneRemovedOfficialAssets(workspace, before, req, projectIDEs, result, nil)
	if _, err := os.Stat(rendered); !os.IsNotExist(err) {
		t.Fatalf("上游已删的资产仍留在 IDE: %v", err)
	}
	if len(result.CleanedAssets) != 1 {
		t.Fatalf("CleanedAssets = %#v", result.CleanedAssets)
	}
}

// 资产还在上游时不能误删。
func TestPruneRemovedOfficialAssetsKeepsSurvivors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	project := t.TempDir()
	cursor := ide.Get("cursor")
	if cursor == nil {
		t.Fatal("cursor IDE 未注册")
	}
	workspace := NewWorkspace(WorkspaceProject, project)
	req := types.RequiresSpec{"playbook": types.RequiresLatest}

	cacheSkill := filepath.Join(project, ".dec", "cache", "playbook", "public", "local", "skills", "kept")
	if err := os.MkdirAll(cacheSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheSkill, "SKILL.md"), []byte("# kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := officialCacheInventory(workspace, req)
	projectIDEs := []ide.IDE{cursor}
	result := &PullProjectAssetsResult{}
	if err := renderOfficialFromCache(workspace, req, projectIDEs, result, nil); err != nil {
		t.Fatal(err)
	}
	rendered := filepath.Join(cursor.SkillsDirForPlane(workspace.IDEPlane(), project, home), managedName("kept"))

	pruneRemovedOfficialAssets(workspace, before, req, projectIDEs, result, nil)
	if _, err := os.Stat(rendered); err != nil {
		t.Fatalf("仍在上游的资产被误删: %v", err)
	}
	if len(result.CleanedAssets) != 0 {
		t.Fatalf("CleanedAssets = %#v", result.CleanedAssets)
	}
}
