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
