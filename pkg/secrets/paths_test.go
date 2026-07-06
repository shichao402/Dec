package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLandingPathForNote_CanonicalNoteName(t *testing.T) {
	got, err := LandingPathForNote("vikunja_workflow", "mise/conf.d/vikunja.toml")
	if err != nil {
		t.Fatal(err)
	}
	want := ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml"
	if got != want {
		t.Fatalf("LandingPathForNote() = %q, want %q", got, want)
	}
}

func TestLandingPathForNote_LongPrefixLegacy(t *testing.T) {
	got, err := LandingPathForNote("vikunja_workflow", ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml")
	if err != nil {
		t.Fatal(err)
	}
	want := ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml"
	if got != want {
		t.Fatalf("LandingPathForNote() = %q, want %q", got, want)
	}
}

func TestLandingPathForNote_LegacyConfigFormat(t *testing.T) {
	got, err := LandingPathForNote("vikunja_workflow", ".config/mise/conf.d/vikunja.toml")
	if err != nil {
		t.Fatal(err)
	}
	want := ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml"
	if got != want {
		t.Fatalf("LandingPathForNote() = %q, want %q", got, want)
	}
}

func TestLandingPathForNote_IntermediateFormat(t *testing.T) {
	got, err := LandingPathForNote("vikunja_workflow", ".secrets/vikunja_workflow/.config/mise/conf.d/vikunja.toml")
	if err != nil {
		t.Fatal(err)
	}
	want := ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml"
	if got != want {
		t.Fatalf("LandingPathForNote() = %q, want %q", got, want)
	}
}

func TestNotePathForBundleFile(t *testing.T) {
	got, err := NotePathForBundleFile("vikunja_workflow", "mise/conf.d/vikunja.toml")
	if err != nil {
		t.Fatal(err)
	}
	want := "mise/conf.d/vikunja.toml"
	if got != want {
		t.Fatalf("NotePathForBundleFile() = %q, want %q", got, want)
	}
}

func TestLegacyNoteName(t *testing.T) {
	longName := ".secrets/vikunja_workflow/mise/conf.d/vikunja.toml"
	got := LegacyNoteName("vikunja_workflow", longName)
	want := "mise/conf.d/vikunja.toml"
	if got != want {
		t.Fatalf("LegacyNoteName() = %q, want %q", got, want)
	}
	if legacy := LegacyNoteName("vikunja_workflow", "mise/conf.d/vikunja.toml"); legacy != longName {
		t.Fatalf("canonical 应映射长前缀: %q", legacy)
	}
}

func TestListSecretsBundleDirNames(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".secrets", "vikunja_workflow", "mise"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, ".secrets", "Dec"), 0755); err != nil {
		t.Fatal(err)
	}
	names, err := ListSecretsBundleDirNames(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "Dec" || names[1] != "vikunja_workflow" {
		t.Fatalf("ListSecretsBundleDirNames() = %#v", names)
	}
}
