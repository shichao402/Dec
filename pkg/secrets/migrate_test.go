package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretsLocalPath(t *testing.T) {
	got, err := SecretsLocalPath("vikunja_workflow", "mise/conf.d/vikunja.toml")
	if err != nil {
		t.Fatal(err)
	}
	want := ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml"
	if got != want {
		t.Fatalf("SecretsLocalPath() = %q, want %q", got, want)
	}
}

func TestNeedsNoteRename(t *testing.T) {
	cases := []struct {
		name   string
		note   string
		rename bool
	}{
		{"legacy root", ".config/mise/conf.d/vikunja.toml", true},
		{"canonical", ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml", false},
		{"intermediate", ".secrets/vikunja_workflow/.config/mise/conf.d/vikunja.toml", true},
	}
	for _, tc := range cases {
		got := NeedsNoteRename("vikunja_workflow", tc.note)
		if got != tc.rename {
			t.Fatalf("%s: NeedsNoteRename(%q) = %v, want %v", tc.name, tc.note, got, tc.rename)
		}
	}
}

func TestCanonicalNotePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{".config/mise/conf.d/vikunja.toml", ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml"},
		{".secrets/vikunja_workflow/.config/mise/conf.d/vikunja.toml", ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml"},
		{".secrets/vikunja_workflow/mise/conf.d/vikunja.toml", ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml"},
	}
	for _, tc := range cases {
		got, err := CanonicalNotePath("vikunja_workflow", tc.in)
		if err != nil {
			t.Fatalf("CanonicalNotePath(%q) err: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("CanonicalNotePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMigrateLocalSecretsFiles_MoveLegacyConfig(t *testing.T) {
	projectRoot := t.TempDir()
	legacyPath := filepath.Join(projectRoot, ".config", "mise", "conf.d", "vikunja.toml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0755); err != nil {
		t.Fatal(err)
	}
	content := "[env]\nTOKEN=abc\n"
	if err := os.WriteFile(legacyPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	moved, err := migrateLocalSecretsFiles(projectRoot, "vikunja", "vikunja_workflow")
	if err != nil {
		t.Fatalf("migrateLocalSecretsFiles() = %v", err)
	}
	if len(moved) != 1 {
		t.Fatalf("moved = %#v, want 1 entry", moved)
	}

	target := filepath.Join(projectRoot, ".secrets", "vikunja_workflow", "mise", "conf.d", "vikunja.toml")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("目标文件不存在: %v", err)
	}
	if string(data) != content {
		t.Fatalf("目标内容 = %q, want %q", string(data), content)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatal("旧路径应已移走")
	}

	movedAgain, err := migrateLocalSecretsFiles(projectRoot, "vikunja", "vikunja_workflow")
	if err != nil {
		t.Fatal(err)
	}
	if len(movedAgain) != 0 {
		t.Fatalf("二次迁移应无变更: %#v", movedAgain)
	}
}

func TestMigrateLocalSecretsFiles_MoveIntermediateSecretsPath(t *testing.T) {
	projectRoot := t.TempDir()
	intermediate := filepath.Join(projectRoot, ".secrets", "vikunja_workflow", ".config", "mise", "conf.d", "vikunja.toml")
	if err := os.MkdirAll(filepath.Dir(intermediate), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(intermediate, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	moved, err := migrateLocalSecretsFiles(projectRoot, "vikunja", "vikunja_workflow")
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 {
		t.Fatalf("moved = %#v", moved)
	}
	target := filepath.Join(projectRoot, ".secrets", "vikunja_workflow", "mise", "conf.d", "vikunja.toml")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("目标文件: %v", err)
	}
}

func TestMigrateLocalSecretsFiles_RemovesDuplicateWhenCanonicalExists(t *testing.T) {
	projectRoot := t.TempDir()
	intermediate := filepath.Join(projectRoot, ".secrets", "vikunja_workflow", ".config", "mise", "conf.d", "vikunja.toml")
	canonical := filepath.Join(projectRoot, ".secrets", "vikunja_workflow", "mise", "conf.d", "vikunja.toml")
	for _, p := range []string{intermediate, canonical} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("secret"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	moved, err := migrateLocalSecretsFiles(projectRoot, "vikunja", "vikunja_workflow")
	if err != nil {
		t.Fatalf("migrateLocalSecretsFiles() = %v", err)
	}
	if len(moved) != 1 {
		t.Fatalf("moved = %#v, want 1 cleanup entry", moved)
	}
	if _, err := os.Stat(intermediate); !os.IsNotExist(err) {
		t.Fatal("中间态路径应已清理")
	}
	if _, err := os.Stat(canonical); err != nil {
		t.Fatalf("规范路径应保留: %v", err)
	}
}

func TestStubClient_MigrateBundle_RenamesLegacyNotes(t *testing.T) {
	client := &StubClient{
		NotesByFolder: map[string][]SecureNote{
			"vikunja_workflow": {{
				RelativePath: ".config/mise/conf.d/vikunja.toml",
				Content:      "[env]\nTOKEN=abc\n",
			}},
		},
	}
	result, err := MigrateBundle(t.Context(), client, MigrateBundleRequest{
		DecBundleName: "vikunja",
		Binding:       BundleBinding{SecretsBundleName: "vikunja_workflow"},
	})
	if err != nil {
		t.Fatalf("MigrateBundle() = %v", err)
	}
	if len(result.RenamedNotes) != 1 {
		t.Fatalf("RenamedNotes = %#v", result.RenamedNotes)
	}
	notes := client.NotesByFolder["vikunja_workflow"]
	if len(notes) != 1 || notes[0].RelativePath != ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml" {
		t.Fatalf("notes = %#v", notes)
	}
}

func TestMigrateConfigIfNeeded_FolderToSecretsBundle(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyYAML := "server_url: https://vault.example.com\nemail: user@example.com\nbundles:\n  - dec_bundle: vikunja\n    folder: vikunja_workflow\n"
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte(legacyYAML), 0600); err != nil {
		t.Fatal(err)
	}

	changed, err := MigrateConfigIfNeeded()
	if err != nil {
		t.Fatalf("MigrateConfigIfNeeded() = %v", err)
	}
	if !changed {
		t.Fatal("应检测到 folder 字段并迁移")
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Bundles) != 1 || cfg.Bundles[0].SecretsBundleName != "vikunja_workflow" {
		t.Fatalf("cfg.Bundles = %#v", cfg.Bundles)
	}
	if cfg.Bundles[0].Folder != "" {
		t.Fatalf("folder 字段应已清除: %#v", cfg.Bundles[0])
	}

	changedAgain, err := MigrateConfigIfNeeded()
	if err != nil {
		t.Fatal(err)
	}
	if changedAgain {
		t.Fatal("二次迁移应无变更")
	}
}
