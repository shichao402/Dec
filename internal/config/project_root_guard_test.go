package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/types"
)

func TestProjectConfigRejectsEmptyProjectRoot(t *testing.T) {
	mgr := NewProjectConfigManager("")

	if _, err := mgr.LoadProjectConfig(); !errors.Is(err, ErrProjectRootRequired) {
		t.Fatalf("LoadProjectConfig() 错误 = %v, 期望 ErrProjectRootRequired", err)
	}
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{}); !errors.Is(err, ErrProjectRootRequired) {
		t.Fatalf("SaveProjectConfig() 错误 = %v, 期望 ErrProjectRootRequired", err)
	}
	if _, err := mgr.EnsureVarsConfigTemplate(); !errors.Is(err, ErrProjectRootRequired) {
		t.Fatalf("EnsureVarsConfigTemplate() 错误 = %v, 期望 ErrProjectRootRequired", err)
	}
	if mgr.Exists() {
		t.Fatal("空项目根不应报告项目配置存在")
	}
}

// 全局配置没有 version 字段，一旦被当成项目配置读取就会触发 v1→v2 升级并被改写，
// repo_url / enabled_bundles 随之丢失。项目根的 .dec/ 落在 Dec 根目录上必须直接拒绝。
func TestProjectConfigRefusesToWriteOverDecHome(t *testing.T) {
	home := t.TempDir()
	decHome := filepath.Join(home, ".dec")
	t.Setenv("DEC_HOME", decHome)

	if err := SaveGlobalConfig(&types.GlobalConfig{
		RepoURL:        "https://example.com/vault.git",
		EnabledBundles: []string{"cli"},
	}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}
	globalPath := filepath.Join(decHome, "config.yaml")
	before, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("读取全局配置失败: %v", err)
	}

	mgr := NewProjectConfigManager(home)
	if mgr.Exists() {
		t.Fatal("Dec 根目录不是项目配置")
	}
	if _, err := mgr.LoadProjectConfig(); err == nil {
		t.Fatal("把全局配置当项目配置读取应报错")
	}
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{IDEs: []string{"cursor"}}); err == nil {
		t.Fatal("往 Dec 根目录写项目配置应报错")
	}

	after, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("重新读取全局配置失败: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("全局配置被改写:\n--- 之前 ---\n%s\n--- 之后 ---\n%s", before, after)
	}
	if !strings.Contains(string(after), "enabled_projects") {
		t.Fatalf("全局配置丢了 enabled_projects:\n%s", after)
	}
}
