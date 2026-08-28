package app

import (
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/types"
)

// 预览不访问 Bitwarden，所以只能报会涉及几个远端地址。
// 待推文件数由远端条目列表决定，不联网就数不出来，也不该猜。
func TestPreviewPushProjectAssets_CountsSecretsTargetsNotFiles(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "demo",
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewPushProjectAssets(projectRoot)
	if err != nil {
		t.Fatalf("PreviewPushProjectAssets() = %v", err)
	}
	if preview.EnabledBundleCount != 1 {
		t.Fatalf("EnabledBundleCount = %d, want 1", preview.EnabledBundleCount)
	}
	// 项目平面 secrets 只有本项目 P 的 private/project 一个落点。
	if preview.SecretsTargetCount != 1 || preview.ProjectSecretsName != "demo/private/project" {
		t.Fatalf("SecretsTargetCount = %d, ProjectSecretsName = %q",
			preview.SecretsTargetCount, preview.ProjectSecretsName)
	}
}
