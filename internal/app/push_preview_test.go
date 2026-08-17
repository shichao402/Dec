package app

import (
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/types"
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
	// 只有 bundle folder：ADR 0014 之后 project 不再是可写归属。
	if preview.SecretsTargetCount != 1 {
		t.Fatalf("SecretsTargetCount = %d, want 1", preview.SecretsTargetCount)
	}
}
