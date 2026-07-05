package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/types"
)

func TestPreviewPushProjectAssets_CountsEnabledAndSecrets(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatal(err)
	}

	secretsFile := filepath.Join(projectRoot, ".secrets", "combo", "tokens", "api.key")
	if err := os.MkdirAll(filepath.Dir(secretsFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretsFile, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewPushProjectAssets(projectRoot)
	if err != nil {
		t.Fatalf("PreviewPushProjectAssets() = %v", err)
	}
	if preview.EnabledBundleCount != 1 {
		t.Fatalf("EnabledBundleCount = %d, want 1", preview.EnabledBundleCount)
	}
	if preview.SecretsFileCount != 1 {
		t.Fatalf("SecretsFileCount = %d, want 1", preview.SecretsFileCount)
	}
}
