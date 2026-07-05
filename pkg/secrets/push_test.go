package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPushBundle_ScansProjectSecretsDir(t *testing.T) {
	projectRoot := t.TempDir()
	projectDir := SecretsBundleDir(projectRoot, "Dec")
	tokenPath := filepath.Join(projectDir, "tokens", "api.key")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("secret-token"), 0600); err != nil {
		t.Fatal(err)
	}

	client := &StubClient{}
	result, err := PushBundle(t.Context(), client, PushBundleRequest{
		ProjectRoot:   projectRoot,
		DecBundleName: ProjectSecretsDecBundleName,
		Binding:       ProjectSecretsBinding("Dec"),
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("Created = %d, want 1", result.Created)
	}
	want := ".secrets/Dec/tokens/api.key"
	if len(result.Paths) != 1 || result.Paths[0] != want {
		t.Fatalf("Paths = %#v, want [%q]", result.Paths, want)
	}
}

func TestPushBundle_ScansLocalFiles(t *testing.T) {
	projectRoot := t.TempDir()
	bundleDir := SecretsBundleDir(projectRoot, "vikunja_workflow")
	confPath := filepath.Join(bundleDir, "mise", "conf.d", "vikunja.toml")
	if err := os.MkdirAll(filepath.Dir(confPath), 0755); err != nil {
		t.Fatal(err)
	}
	content := "[env]\nTOKEN=abc\n"
	if err := os.WriteFile(confPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	client := &StubClient{}
	result, err := PushBundle(t.Context(), client, PushBundleRequest{
		ProjectRoot:   projectRoot,
		DecBundleName: "vikunja",
		Binding:       BundleBinding{DecBundleName: "vikunja", SecretsBundleName: "vikunja_workflow"},
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("Created = %d, want 1", result.Created)
	}
	if len(result.Paths) != 1 || result.Paths[0] != ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml" {
		t.Fatalf("Paths = %#v", result.Paths)
	}
}

func TestPushBundle_EmptyDir(t *testing.T) {
	projectRoot := t.TempDir()
	client := &StubClient{}
	result, err := PushBundle(t.Context(), client, PushBundleRequest{
		ProjectRoot:   projectRoot,
		DecBundleName: "vikunja",
		Binding:       BundleBinding{SecretsBundleName: "vikunja_workflow"},
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if result.Created != 0 || result.Updated != 0 {
		t.Fatalf("result = %#v, 期望空结果", result)
	}
}
