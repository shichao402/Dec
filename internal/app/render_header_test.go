package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderedHeaderProviderUsesProvidesRoot(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, `kind: project
version: v2
layout_version: 1
project_name: relkit
provides_root: DecAssets
provides:
  relkit-ops:
    source: DecAssets/skills/relkit-ops
    visibility: public
    plane: local
    type: skill
    name: relkit-ops
`)
	ws := NewWorkspace(WorkspaceProject, root)
	info := renderHeaderInfoFor(ws, "relkit")
	if info.Mode != renderHeaderProvider || info.EditRoot != "DecAssets" {
		t.Fatalf("info = %+v, want provider DecAssets", info)
	}
	header := renderedHeader(info)
	if !strings.Contains(header, "DecAssets/") || !strings.Contains(header, "publish-provides") {
		t.Fatalf("header missing provider guidance:\n%s", header)
	}
	if strings.Contains(header, "Run 页 push → pull") {
		t.Fatalf("provider header must not recommend Run-page push:\n%s", header)
	}
}

func TestRenderedHeaderOfficialIsReadonly(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, `kind: project
version: v2
layout_version: 1
project_name: loom
requires:
  relkit: latest
`)
	ws := NewWorkspace(WorkspaceProject, root)
	info := renderHeaderInfoFor(ws, "relkit")
	if info.Mode != renderHeaderOfficial {
		t.Fatalf("mode = %v, want official", info.Mode)
	}
	header := renderedHeader(info)
	if !strings.Contains(header, "Dec registry") || !strings.Contains(header, "本地覆写") {
		t.Fatalf("header missing official guidance:\n%s", header)
	}
	if strings.Contains(header, ".dec/cache/relkit") {
		t.Fatalf("official header must not point at cache edit path:\n%s", header)
	}
}

func TestRenderedHeaderVaultKeepsCachePushRoute(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, `kind: project
version: v2
layout_version: 1
project_name: notes
requires:
  personal-kit: vault
`)
	ws := NewWorkspace(WorkspaceProject, root)
	info := renderHeaderInfoFor(ws, "personal-kit")
	if info.Mode != renderHeaderVault {
		t.Fatalf("mode = %v, want vault", info.Mode)
	}
	header := renderedHeader(info)
	if !strings.Contains(header, ".dec/cache/personal-kit/") || !strings.Contains(header, "Run 页 push") {
		t.Fatalf("vault header missing cache push route:\n%s", header)
	}
}

func TestInjectRenderedHeaderReplacesStaleCacheGuidance(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, `kind: project
version: v2
layout_version: 1
project_name: relkit
provides_root: DecAssets
provides:
  relkit-ops:
    source: DecAssets/skills/relkit-ops
    visibility: public
    plane: local
    type: skill
    name: relkit-ops
`)
	skill := filepath.Join(root, "SKILL.md")
	stale := "<!-- 本文件由 `dec pull` 从 .dec/cache/relkit/ 渲染生成，请勿直接编辑。\n" +
		"     修改流程：编辑 .dec/cache/relkit/... → 在 Run 页 push → pull 验证 -->\n\n" +
		"# relkit 运维\n"
	if err := os.WriteFile(skill, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := NewWorkspace(WorkspaceProject, root)
	if err := injectRenderedHeaderFile(skill, ws, "relkit"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Count(text, renderedHeaderMarker) != 1 {
		t.Fatalf("expected one header marker, got:\n%s", text)
	}
	if !strings.Contains(text, "DecAssets/") || strings.Contains(text, ".dec/cache/relkit/") {
		t.Fatalf("stale cache guidance was not replaced:\n%s", text)
	}
	if !strings.HasSuffix(strings.TrimSpace(text), "# relkit 运维") && !strings.Contains(text, "# relkit 运维\n") {
		t.Fatalf("body lost:\n%s", text)
	}
}

func TestStripRenderedHeaderLeavesForeignComments(t *testing.T) {
	body := []byte("<!-- license -->\n\n# Title\n")
	got := stripRenderedHeader(body)
	if string(got) != string(body) {
		t.Fatalf("stripped foreign comment: %q", got)
	}
}

func writeProjectConfig(t *testing.T, root, body string) {
	t.Helper()
	dec := filepath.Join(root, ".dec")
	if err := os.MkdirAll(dec, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dec, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
