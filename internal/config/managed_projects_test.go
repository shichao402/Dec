package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedProjectsRegisterDeduplicateAndRemove(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	first, err := RegisterManagedProject(root, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterManagedProject(filepath.Join(root, "."), "updated"); err != nil {
		t.Fatal(err)
	}
	items, err := ListManagedProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Label != "updated" || items[0].Root != first.Root {
		t.Fatalf("unexpected projects: %#v", items)
	}
	removed, err := RemoveManagedProject(root)
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("remove registry must not remove project directory: %v", err)
	}
}
