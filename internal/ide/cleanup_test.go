package ide

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveDecMCPEntriesPreservesSharedJSONFields(t *testing.T) {
	home := t.TempDir()
	impl := Get("cursor")
	path := impl.MCPConfigPathForPlane(PlaneUser, "", home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"mcpServers":{"dec":{"command":"dec-mcp"},"dec-tool":{"command":"x"},"user":{"command":"y"}},"transportType":"stdio"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	names, err := RemoveDecMCPEntries(impl, PlaneUser, "", home)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("removed = %#v", names)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `"dec"`) || strings.Contains(text, `"dec-tool"`) ||
		!strings.Contains(text, `"user"`) || !strings.Contains(text, `"transportType"`) {
		t.Fatalf("unexpected config: %s", text)
	}
}

func TestProjectCleanupDoesNotRemoveBuiltinDecName(t *testing.T) {
	root := t.TempDir()
	impl := Get("cursor")
	path := impl.MCPConfigPathForPlane(PlaneProject, root, "")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"dec":{"command":"user"},"dec-owned":{"command":"x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveDecMCPEntries(impl, PlaneProject, root, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"dec"`) || strings.Contains(string(data), `"dec-owned"`) {
		t.Fatalf("unexpected project config: %s", data)
	}
}

func TestPurgeRemovedInternalHomes(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{".claude-internal", ".codex-internal"} {
		dir := filepath.Join(home, name, "skills")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "x.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	keep := filepath.Join(home, ".claude", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	notes := PurgeRemovedInternalHomes(home)
	if len(notes) != 2 {
		t.Fatalf("notes = %#v", notes)
	}
	for _, name := range []string{".claude-internal", ".codex-internal"} {
		if _, err := os.Stat(filepath.Join(home, name)); !os.IsNotExist(err) {
			t.Fatalf("%s 应已删除, err=%v", name, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("~/.claude 应保留: %v", err)
	}
	if notes := PurgeRemovedInternalHomes(home); len(notes) != 0 {
		t.Fatalf("幂等 notes = %#v", notes)
	}
}
