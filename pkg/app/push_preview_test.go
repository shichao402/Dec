package app

import (
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/types"
)

// 预览不访问 Bitwarden，所以只能报会涉及几个 folder。
// 待推文件数由远端 folder 的 note 列表决定，不联网就数不出来，也不该猜。
func TestPreviewPushProjectAssets_CountsSecretsTargetsNotFiles(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "Demo",
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
	// 1 个 bundle folder + 1 个 project folder。
	if preview.SecretsTargetCount != 2 {
		t.Fatalf("SecretsTargetCount = %d, want 2", preview.SecretsTargetCount)
	}
}
