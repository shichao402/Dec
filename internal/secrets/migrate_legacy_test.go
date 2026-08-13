package secrets

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateLegacyLocalSecrets_TomlToDotEnv(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, filepath.FromSlash(".config/mise/conf.d/vikunja.toml"))
	if err := os.MkdirAll(filepath.Dir(old), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("[env]\nTOKEN = \"abc\"\nURL = 'https://x'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	target, err := NewBundleSyncTarget("vikunja", "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := MigrateLegacyLocalSecrets(root, []LegacyLocalSecret{{
		OldProjectRel: ".config/mise/conf.d/vikunja.toml",
		Target:        target,
		NoteRel:       "env/vikunja.env",
	}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Moved) != 1 || len(res.Removed) != 1 {
		t.Fatalf("result = %#v", res)
	}
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path.Join(target.LocalRoot, "env/vikunja.env"))))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "TOKEN=abc") || !strings.Contains(s, "URL=https://x") {
		t.Fatalf("dotenv = %q", s)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("旧文件应已删除")
	}
}

func TestDefaultLegacyLocalMigrations_IncludesEnabledBundles(t *testing.T) {
	items, err := DefaultLegacyLocalMigrations([]string{"vikunja", "tencent-cloud"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 2 {
		t.Fatalf("items = %#v", items)
	}
	found := false
	for _, it := range items {
		if it.Target.Name == "vikunja" && strings.HasSuffix(it.NoteRel, "vikunja.env") {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少 vikunja 映射: %#v", items)
	}
}
