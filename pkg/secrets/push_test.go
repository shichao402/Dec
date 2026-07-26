package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func writeProjectFile(t *testing.T, projectRoot, rel, content string) {
	t.Helper()
	path := filepath.Join(projectRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPushBundle_ReadsLocalFilesNamedByRemoteNotes(t *testing.T) {
	projectRoot := t.TempDir()
	writeProjectFile(t, projectRoot, ".config/mise/conf.d/vikunja.toml", "[env]\nTOKEN=abc\n")

	// 远端 folder 的 note 列表是权威索引：push 按它去读本地对应路径。
	client := &StubClient{NotesByFolder: map[string][]SecureNote{
		"vikunja_workflow": {{RelativePath: ".config/mise/conf.d/vikunja.toml", Content: "[env]\nTOKEN=old\n"}},
	}}
	result, err := PushBundle(t.Context(), client, PushBundleRequest{
		ProjectRoot:   projectRoot,
		DecBundleName: "vikunja",
		Binding:       BundleBinding{DecBundleName: "vikunja", SecretsBundleName: "vikunja_workflow"},
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if result.Created != 0 || result.Updated != 1 {
		t.Fatalf("result = %#v, 期望 1 条更新", result)
	}
	if len(result.Paths) != 1 || result.Paths[0] != ".config/mise/conf.d/vikunja.toml" {
		t.Fatalf("Paths = %#v", result.Paths)
	}
	if got := client.NotesByFolder["vikunja_workflow"][0].Content; got != "[env]\nTOKEN=abc\n" {
		t.Fatalf("远端正文未被本地覆盖: %q", got)
	}
}

func TestPushBundle_ReportsMissingLocalWithoutDeletingRemote(t *testing.T) {
	projectRoot := t.TempDir()
	writeProjectFile(t, projectRoot, ".env.local", "TOKEN=abc\n")

	client := &StubClient{NotesByFolder: map[string][]SecureNote{
		"my-project": {
			{RelativePath: ".env.local", Content: "old"},
			{RelativePath: "config/private.yaml", Content: "只在远端存在"},
		},
	}}
	result, err := PushBundle(t.Context(), client, PushBundleRequest{
		ProjectRoot:   projectRoot,
		DecBundleName: ProjectSecretsDecBundleName,
		Binding:       ProjectSecretsBinding("my-project"),
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if len(result.MissingLocal) != 1 || result.MissingLocal[0] != "config/private.yaml" {
		t.Fatalf("MissingLocal = %#v, 期望 [config/private.yaml]", result.MissingLocal)
	}
	// 本地缺文件只报告：远端那条必须原样留着，绝不能被 push 删掉。
	if len(client.NotesByFolder["my-project"]) != 2 {
		t.Fatalf("远端 note 数 = %d, 期望 2（不删孤儿）", len(client.NotesByFolder["my-project"]))
	}
}

func TestPushBundle_EmptyRemoteFolder(t *testing.T) {
	result, err := PushBundle(t.Context(), &StubClient{}, PushBundleRequest{
		ProjectRoot:   t.TempDir(),
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

func TestPushBundle_RejectsIllegalRemoteNoteName(t *testing.T) {
	client := &StubClient{NotesByFolder: map[string][]SecureNote{
		"evil": {{RelativePath: "../../etc/passwd", Content: "x"}},
	}}
	_, err := PushBundle(t.Context(), client, PushBundleRequest{
		ProjectRoot:   t.TempDir(),
		DecBundleName: "evil",
		Binding:       BundleBinding{SecretsBundleName: "evil"},
	})
	if err == nil {
		t.Fatal("远端 note 名逃逸项目根时应报错")
	}
}

func TestAddSecureNote_CreatesNoteNamedByLandingPath(t *testing.T) {
	projectRoot := t.TempDir()
	writeProjectFile(t, projectRoot, "config/private.yaml", "token: abc\n")

	client := &StubClient{}
	if err := AddSecureNote(t.Context(), client, projectRoot, "tencent-cloud", "config/private.yaml"); err != nil {
		t.Fatalf("AddSecureNote() = %v", err)
	}
	notes := client.NotesByFolder["tencent-cloud"]
	if len(notes) != 1 {
		t.Fatalf("notes = %#v", notes)
	}
	if notes[0].RelativePath != "config/private.yaml" {
		t.Fatalf("note 名 = %q, 期望即落地路径", notes[0].RelativePath)
	}
	if notes[0].Content != "token: abc\n" {
		t.Fatalf("正文 = %q", notes[0].Content)
	}
}

func TestAddSecureNote_RejectsPathOutsideProject(t *testing.T) {
	err := AddSecureNote(t.Context(), &StubClient{}, t.TempDir(), "tencent-cloud", "../outside.yaml")
	if err == nil {
		t.Fatal("落地路径逃逸项目根时应报错")
	}
}

func TestAddSecureNote_RejectsDecOverlap(t *testing.T) {
	projectRoot := t.TempDir()
	writeProjectFile(t, projectRoot, ".dec/config.yaml", "x")

	err := AddSecureNote(t.Context(), &StubClient{}, projectRoot, "evil", ".dec/config.yaml")
	if err == nil {
		t.Fatal("落地路径落进 .dec/ 时应报错")
	}
}
