package vars

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

func TestExtractPlaceholders(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "no placeholders",
			content: "hello world",
			want:    nil,
		},
		{
			name:    "single placeholder",
			content: "url: {{VIKUNJA_URL}}",
			want:    []string{"VIKUNJA_URL"},
		},
		{
			name:    "multiple placeholders",
			content: "url: {{VIKUNJA_URL}}, token: {{API_TOKEN}}",
			want:    []string{"VIKUNJA_URL", "API_TOKEN"},
		},
		{
			name:    "duplicate placeholders",
			content: "{{FOO}} and {{FOO}} again",
			want:    []string{"FOO"},
		},
		{
			name:    "placeholder in JSON",
			content: `{"env": {"URL": "{{MY_URL}}", "TOKEN": "{{MY_TOKEN}}"}}`,
			want:    []string{"MY_URL", "MY_TOKEN"},
		},
		{
			name:    "invalid var name - lowercase",
			content: "{{lowercase}}",
			want:    nil,
		},
		{
			name:    "placeholder with numbers",
			content: "{{VAR1}} {{V2_TEST}}",
			want:    []string{"VAR1", "V2_TEST"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractPlaceholders(tt.content)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractPlaceholders() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasPlaceholders(t *testing.T) {
	if HasPlaceholders("no placeholders here") {
		t.Error("expected false for plain text")
	}
	if !HasPlaceholders("has {{PLACEHOLDER}}") {
		t.Error("expected true for text with placeholder")
	}
}

func TestSubstitute(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		vars        map[string]string
		wantResult  string
		wantUsed    map[string]string
		wantMissing []string
	}{
		{
			name:       "full substitution",
			content:    "url: {{MY_URL}}, token: {{MY_TOKEN}}",
			vars:       map[string]string{"MY_URL": "http://example.com", "MY_TOKEN": "abc123"},
			wantResult: "url: http://example.com, token: abc123",
			wantUsed:   map[string]string{"MY_URL": "http://example.com", "MY_TOKEN": "abc123"},
		},
		{
			name:        "partial substitution",
			content:     "url: {{MY_URL}}, token: {{MY_TOKEN}}",
			vars:        map[string]string{"MY_URL": "http://example.com"},
			wantResult:  "url: http://example.com, token: {{MY_TOKEN}}",
			wantUsed:    map[string]string{"MY_URL": "http://example.com"},
			wantMissing: []string{"MY_TOKEN"},
		},
		{
			name:       "no placeholders",
			content:    "plain text",
			vars:       map[string]string{"FOO": "bar"},
			wantResult: "plain text",
			wantUsed:   map[string]string{},
		},
		{
			name:        "all missing",
			content:     "{{UNKNOWN_VAR}}",
			vars:        map[string]string{},
			wantResult:  "{{UNKNOWN_VAR}}",
			wantUsed:    map[string]string{},
			wantMissing: []string{"UNKNOWN_VAR"},
		},
		{
			name:       "JSON env substitution",
			content:    `{"VIKUNJA_URL": "{{VIKUNJA_URL}}", "VIKUNJA_API_TOKEN": "{{VIKUNJA_API_TOKEN}}"}`,
			vars:       map[string]string{"VIKUNJA_URL": "http://vikunja.local/api/v1", "VIKUNJA_API_TOKEN": "tk_test"},
			wantResult: `{"VIKUNJA_URL": "http://vikunja.local/api/v1", "VIKUNJA_API_TOKEN": "tk_test"}`,
			wantUsed:   map[string]string{"VIKUNJA_URL": "http://vikunja.local/api/v1", "VIKUNJA_API_TOKEN": "tk_test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotResult, gotUsed, gotMissing := Substitute(tt.content, tt.vars)
			if gotResult != tt.wantResult {
				t.Errorf("result = %q, want %q", gotResult, tt.wantResult)
			}
			if !reflect.DeepEqual(gotUsed, tt.wantUsed) {
				t.Errorf("used = %v, want %v", gotUsed, tt.wantUsed)
			}
			sort.Strings(gotMissing)
			sort.Strings(tt.wantMissing)
			if len(gotMissing) > 0 || len(tt.wantMissing) > 0 {
				if !reflect.DeepEqual(gotMissing, tt.wantMissing) {
					t.Errorf("missing = %v, want %v", gotMissing, tt.wantMissing)
				}
			}
		})
	}
}

func TestRestore(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		varsUsed map[string]string
		want     string
	}{
		{
			name:     "basic restore",
			content:  "url: http://example.com, token: abc123",
			varsUsed: map[string]string{"MY_URL": "http://example.com", "MY_TOKEN": "abc123"},
			want:     "url: {{MY_URL}}, token: {{MY_TOKEN}}",
		},
		{
			name:     "empty varsUsed",
			content:  "unchanged",
			varsUsed: nil,
			want:     "unchanged",
		},
		{
			name:     "empty value skipped",
			content:  "foo bar",
			varsUsed: map[string]string{"FOO": ""},
			want:     "foo bar",
		},
		{
			name:     "longer values replaced first",
			content:  "http://vikunja.example.com/api/v1 and http://vikunja.example.com",
			varsUsed: map[string]string{"FULL_URL": "http://vikunja.example.com/api/v1", "BASE_URL": "http://vikunja.example.com"},
			want:     "{{FULL_URL}} and {{BASE_URL}}",
		},
		{
			name:     "JSON restore",
			content:  `{"VIKUNJA_URL": "http://vikunja.local/api/v1"}`,
			varsUsed: map[string]string{"VIKUNJA_URL": "http://vikunja.local/api/v1"},
			want:     `{"VIKUNJA_URL": "{{VIKUNJA_URL}}"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Restore(tt.content, tt.varsUsed)
			if got != tt.want {
				t.Errorf("Restore() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveVars(t *testing.T) {
	global := &types.VarsConfig{
		Vars: map[string]string{
			"URL":   "global-url",
			"TOKEN": "global-token",
			"EXTRA": "global-extra",
		},
	}

	project := &types.VarsConfig{
		Vars: map[string]string{
			"URL":   "project-url",
			"TOKEN": "project-token",
		},
		Assets: &types.AssetVars{
			MCPs: map[string]types.AssetVarEntry{
				"vikunja-mcp": {
					Vars: map[string]string{
						"URL": "asset-url",
					},
				},
			},
		},
	}

	tests := []struct {
		name         string
		assetType    string
		assetName    string
		placeholders []string
		want         map[string]string
	}{
		{
			name:         "asset specific wins",
			assetType:    "mcp",
			assetName:    "vikunja-mcp",
			placeholders: []string{"URL"},
			want:         map[string]string{"URL": "asset-url"},
		},
		{
			name:         "project wins over global",
			assetType:    "mcp",
			assetName:    "other-mcp",
			placeholders: []string{"URL", "TOKEN"},
			want:         map[string]string{"URL": "project-url", "TOKEN": "project-token"},
		},
		{
			name:         "falls through to global",
			assetType:    "rule",
			assetName:    "some-rule",
			placeholders: []string{"EXTRA"},
			want:         map[string]string{"EXTRA": "global-extra"},
		},
		{
			name:         "missing variable not in result",
			assetType:    "rule",
			assetName:    "some-rule",
			placeholders: []string{"NONEXISTENT"},
			want:         map[string]string{},
		},
		{
			name:         "mixed priority",
			assetType:    "mcp",
			assetName:    "vikunja-mcp",
			placeholders: []string{"URL", "TOKEN", "EXTRA"},
			want:         map[string]string{"URL": "asset-url", "TOKEN": "project-token", "EXTRA": "global-extra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveVars(global, project, tt.assetType, tt.assetName, tt.placeholders)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ResolveVars() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveVarsNilConfigs(t *testing.T) {
	got := ResolveVars(nil, nil, "rule", "test", []string{"FOO"})
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestSubstituteFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.mdc")

	content := "url: {{MY_URL}}\ntoken: {{MY_TOKEN}}\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	vars := map[string]string{"MY_URL": "http://example.com", "MY_TOKEN": "secret"}
	used, missing, err := SubstituteFile(filePath, vars)
	if err != nil {
		t.Fatal(err)
	}

	if len(missing) != 0 {
		t.Errorf("unexpected missing: %v", missing)
	}
	if used["MY_URL"] != "http://example.com" || used["MY_TOKEN"] != "secret" {
		t.Errorf("unexpected used: %v", used)
	}

	// Verify file contents
	data, _ := os.ReadFile(filePath)
	expected := "url: http://example.com\ntoken: secret\n"
	if string(data) != expected {
		t.Errorf("file content = %q, want %q", string(data), expected)
	}
}

func TestSubstituteFileNoPlaceholders(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "plain.txt")

	if err := os.WriteFile(filePath, []byte("no placeholders"), 0644); err != nil {
		t.Fatal(err)
	}

	used, missing, err := SubstituteFile(filePath, map[string]string{"FOO": "bar"})
	if err != nil {
		t.Fatal(err)
	}
	if used != nil || missing != nil {
		t.Errorf("expected nil, got used=%v missing=%v", used, missing)
	}
}

func TestRestoreFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.mdc")

	content := "url: http://example.com\ntoken: secret\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	varsUsed := map[string]string{"MY_URL": "http://example.com", "MY_TOKEN": "secret"}
	if err := RestoreFile(filePath, varsUsed); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filePath)
	expected := "url: {{MY_URL}}\ntoken: {{MY_TOKEN}}\n"
	if string(data) != expected {
		t.Errorf("file content = %q, want %q", string(data), expected)
	}
}

func TestSubstituteDir(t *testing.T) {
	dir := t.TempDir()

	// Create nested files
	subDir := filepath.Join(dir, "sub")
	os.MkdirAll(subDir, 0755)

	os.WriteFile(filepath.Join(dir, "a.md"), []byte("{{FOO}}"), 0644)
	os.WriteFile(filepath.Join(subDir, "b.md"), []byte("{{BAR}}"), 0644)

	vars := map[string]string{"FOO": "foo-val", "BAR": "bar-val"}
	used, missing, err := SubstituteDir(dir, vars)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Errorf("unexpected missing: %v", missing)
	}
	if used["FOO"] != "foo-val" || used["BAR"] != "bar-val" {
		t.Errorf("unexpected used: %v", used)
	}

	data1, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	data2, _ := os.ReadFile(filepath.Join(subDir, "b.md"))
	if string(data1) != "foo-val" || string(data2) != "bar-val" {
		t.Errorf("files not substituted correctly")
	}
}

func TestRestoreDir(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	os.MkdirAll(subDir, 0755)

	os.WriteFile(filepath.Join(dir, "a.md"), []byte("foo-val"), 0644)
	os.WriteFile(filepath.Join(subDir, "b.md"), []byte("bar-val"), 0644)

	varsUsed := map[string]string{"FOO": "foo-val", "BAR": "bar-val"}
	if err := RestoreDir(dir, varsUsed); err != nil {
		t.Fatal(err)
	}

	data1, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	data2, _ := os.ReadFile(filepath.Join(subDir, "b.md"))
	if string(data1) != "{{FOO}}" || string(data2) != "{{BAR}}" {
		t.Errorf("files not restored correctly: a=%q b=%q", string(data1), string(data2))
	}
}

func TestLoadVarsFile(t *testing.T) {
	t.Run("file not found", func(t *testing.T) {
		cfg, err := LoadVarsFile("/nonexistent/vars.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if cfg == nil || cfg.Vars != nil {
			t.Errorf("expected empty config, got %+v", cfg)
		}
	})

	t.Run("valid file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "vars.yaml")
		content := `vars:
  URL: "http://example.com"
  TOKEN: "secret"
assets:
  mcp:
    my-mcp:
      vars:
        URL: "http://override.com"
`
		os.WriteFile(path, []byte(content), 0644)

		cfg, err := LoadVarsFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Vars["URL"] != "http://example.com" {
			t.Errorf("URL = %q, want %q", cfg.Vars["URL"], "http://example.com")
		}
		if cfg.Assets == nil || cfg.Assets.MCPs["my-mcp"].Vars["URL"] != "http://override.com" {
			t.Error("asset-specific vars not loaded correctly")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "vars.yaml")
		os.WriteFile(path, []byte("invalid: [yaml: {"), 0644)

		_, err := LoadVarsFile(path)
		if err == nil {
			t.Error("expected error for invalid yaml")
		}
	})
}

func TestRoundTrip(t *testing.T) {
	// Simulate: template -> substitute -> restore -> should equal original
	template := `# Config
URL: {{VIKUNJA_URL}}
Token: {{VIKUNJA_API_TOKEN}}
Project: {{PROJECT_NAME}}
`
	vars := map[string]string{
		"VIKUNJA_URL":       "http://vikunja.local/api/v1",
		"VIKUNJA_API_TOKEN": "tk_abc123",
		"PROJECT_NAME":      "my-project",
	}

	// Substitute
	substituted, used, missing := Substitute(template, vars)
	if len(missing) != 0 {
		t.Errorf("unexpected missing: %v", missing)
	}

	// Restore
	restored := Restore(substituted, used)
	if restored != template {
		t.Errorf("round trip failed:\n  original: %q\n  restored: %q", template, restored)
	}
}
