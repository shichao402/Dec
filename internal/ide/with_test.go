package ide

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/types"
)

func TestWithPaths(t *testing.T) {
	impl := Get("with")
	if impl.Name() != "with" {
		t.Fatalf("Name = %q", impl.Name())
	}

	projectRoot := filepath.Join("/project")
	home := filepath.Join("/home", "dev")

	if got := impl.PlaneRoot(PlaneProject, projectRoot, home); got != filepath.Join(projectRoot, ".with") {
		t.Fatalf("project root = %s", got)
	}
	if got := impl.PlaneRoot(PlaneUser, projectRoot, home); got != filepath.Join(home, ".bg-agent", "config-with-app") {
		t.Fatalf("user root = %s", got)
	}
	if got := impl.SkillsDirForPlane(PlaneUser, projectRoot, home); got != filepath.Join(home, ".bg-agent", "config-with-app", "skills") {
		t.Fatalf("user skills = %s", got)
	}
	if got := impl.RulesDirForPlane(PlaneUser, projectRoot, home); got != filepath.Join(home, ".bg-agent", "config-with-app", "rules") {
		t.Fatalf("user rules = %s", got)
	}
	if got := impl.MCPConfigPathForPlane(PlaneUser, projectRoot, home); got != filepath.Join(home, ".bg-agent", "config-with-app", "mcp_config.json") {
		t.Fatalf("user mcp = %s", got)
	}
	if got := impl.MCPConfigPath(projectRoot); got != filepath.Join(projectRoot, ".with", "mcp_config.json") {
		t.Fatalf("project mcp = %s", got)
	}
}

func TestWithWriteMCPConfigPreservesForeignServers(t *testing.T) {
	root := t.TempDir()
	impl := newWithIDE()
	configPath := impl.MCPConfigPath(root)

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	existing := `{
  "mcpServers": {
    "gongfeng": {
      "disabled": false,
      "headers": {"Authorization": "<tai_token>"},
      "timeout": 60,
      "transportType": "streamable-http",
      "url": "https://example.woa.com/gongfeng"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := impl.WriteMCPConfig(root, &types.MCPConfig{MCPServers: map[string]types.MCPServer{
		"dec-demo": {Command: "dec-exec", Args: []string{"--demo"}},
		"gongfeng": {URL: "https://should-not-overwrite.example"},
	}}); err != nil {
		t.Fatalf("WriteMCPConfig: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	gongfeng := parsed.MCPServers["gongfeng"]
	if gongfeng["url"] != "https://example.woa.com/gongfeng" {
		t.Fatalf("foreign url overwritten: %#v", gongfeng)
	}
	if gongfeng["transportType"] != "streamable-http" {
		t.Fatalf("transportType lost: %#v", gongfeng)
	}
	demo := parsed.MCPServers["dec-demo"]
	if demo["command"] != "dec-exec" {
		t.Fatalf("managed command missing: %#v", demo)
	}
	if demo["disabled"] != false {
		t.Fatalf("managed disabled default = %#v", demo["disabled"])
	}
}

func TestWithLoadMCPConfigMapsDisabledAndHeaders(t *testing.T) {
	root := t.TempDir()
	impl := newWithIDE()
	configPath := impl.MCPConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	content := `{
  "mcpServers": {
    "svc": {
      "disabled": true,
      "headers": {"X-Token": "abc"},
      "timeout": 45,
      "transportType": "streamable-http",
      "url": "https://example.test/mcp",
      "description": "demo"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := impl.LoadMCPConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	server := cfg.MCPServers["svc"]
	if server.URL != "https://example.test/mcp" {
		t.Fatalf("url = %q", server.URL)
	}
	if server.Enabled == nil || *server.Enabled {
		t.Fatalf("enabled = %#v, want false", server.Enabled)
	}
	if server.HTTPHeaders["X-Token"] != "abc" {
		t.Fatalf("headers = %#v", server.HTTPHeaders)
	}
	if server.ToolTimeoutSec == nil || *server.ToolTimeoutSec != 45 {
		t.Fatalf("timeout = %#v", server.ToolTimeoutSec)
	}
	if server.Extra["transportType"] != "streamable-http" {
		t.Fatalf("extra transportType = %#v", server.Extra)
	}
	if server.Extra["description"] != "demo" {
		t.Fatalf("extra description = %#v", server.Extra)
	}
}

func TestWithWriteMCPConfigRemovesManagedOnly(t *testing.T) {
	root := t.TempDir()
	impl := newWithIDE()
	configPath := impl.MCPConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {
    "keep": {"url": "https://keep.test", "transportType": "streamable-http", "disabled": false},
    "dec-old": {"command": "old"}
  }
}`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := impl.WriteMCPConfig(root, &types.MCPConfig{MCPServers: map[string]types.MCPServer{}}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed.MCPServers["keep"]; !ok {
		t.Fatalf("keep missing: %s", data)
	}
	if _, ok := parsed.MCPServers["dec-old"]; ok {
		t.Fatalf("managed entry should be removed: %s", data)
	}
}
