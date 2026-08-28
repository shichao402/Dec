package secrets

import (
	"os"
	"path"
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

func TestPushBundle_ScansLocalSyncRoot(t *testing.T) {
	projectRoot := t.TempDir()
	target, err := NewPSyncTarget("vikunja", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, projectRoot, path.Join(target.LocalRoot, "env/vikunja.env"), "TOKEN=abc\n")

	client := &StubClient{NotesByFolder: map[string][]SecureNote{
		target.Address: {{RelativePath: "env/vikunja.env", Content: "TOKEN=old\n"}},
	}}
	result, err := PushBundle(t.Context(), client, PushBundleRequest{
		ProjectRoot: projectRoot,
		Target:      target,
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if result.Created != 0 || result.Updated != 1 {
		t.Fatalf("result = %#v, 期望 1 条更新", result)
	}
	if len(result.Paths) != 1 || result.Paths[0] != "env/vikunja.env" {
		t.Fatalf("Paths = %#v", result.Paths)
	}
	if got := client.NotesByFolder[target.Address][0].Content; got != "TOKEN=abc\n" {
		t.Fatalf("远端正文未被本地覆盖: %q", got)
	}
}

func TestPushBundle_CreatesNewLocalFiles(t *testing.T) {
	projectRoot := t.TempDir()
	target, err := NewPSyncTarget("vikunja", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, projectRoot, path.Join(target.LocalRoot, "env/vikunja.env"), "TOKEN=new\n")

	client := &StubClient{}
	result, err := PushBundle(t.Context(), client, PushBundleRequest{
		ProjectRoot: projectRoot,
		Target:      target,
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if result.Created != 1 || result.Updated != 0 {
		t.Fatalf("result = %#v, 期望 create 1", result)
	}
	notes := client.NotesByFolder[target.Address]
	if len(notes) != 1 || notes[0].RelativePath != "env/vikunja.env" {
		t.Fatalf("notes = %#v", notes)
	}
}

func TestPushBundle_ReportsMissingLocalWithoutDeletingRemote(t *testing.T) {
	projectRoot := t.TempDir()
	target, err := NewPSyncTarget("my-project", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, projectRoot, path.Join(target.LocalRoot, "env/app.env"), "TOKEN=abc\n")

	client := &StubClient{NotesByFolder: map[string][]SecureNote{
		target.Address: {
			{RelativePath: "env/app.env", Content: "old"},
			{RelativePath: "config/private.yaml", Content: "只在远端存在"},
		},
	}}
	result, err := PushBundle(t.Context(), client, PushBundleRequest{
		ProjectRoot: projectRoot,
		Target:      target,
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if len(result.MissingLocal) != 1 || result.MissingLocal[0] != "config/private.yaml" {
		t.Fatalf("MissingLocal = %#v, 期望 [config/private.yaml]", result.MissingLocal)
	}
	if len(client.NotesByFolder[target.Address]) != 2 {
		t.Fatalf("远端 note 数 = %d, 期望 2（不删孤儿）", len(client.NotesByFolder[target.Address]))
	}
}

func TestPushBundle_EmptyLocalRoot(t *testing.T) {
	target, err := NewPSyncTarget("vikunja", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	result, err := PushBundle(t.Context(), &StubClient{}, PushBundleRequest{
		ProjectRoot: t.TempDir(),
		Target:      target,
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if result.Created != 0 || result.Updated != 0 {
		t.Fatalf("result = %#v, 期望空结果", result)
	}
}

func TestPushBundle_KeepsRemoteOrphanWithEscapingName(t *testing.T) {
	projectRoot := t.TempDir()
	target, err := NewPSyncTarget("evil", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, projectRoot, path.Join(target.LocalRoot, "ok.env"), "x")
	// 远端孤儿带逃逸名也不应删；本地扫描路径正常时 push 应成功。
	client := &StubClient{NotesByFolder: map[string][]SecureNote{
		target.Address: {{RelativePath: "../../etc/passwd", Content: "x"}},
	}}
	result, err := PushBundle(t.Context(), client, PushBundleRequest{
		ProjectRoot: projectRoot,
		Target:      target,
	})
	if err != nil {
		t.Fatalf("本地合法文件 push 不应因远端脏名失败: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(result.MissingLocal) != 1 || result.MissingLocal[0] != "../../etc/passwd" {
		t.Fatalf("MissingLocal = %#v", result.MissingLocal)
	}
}

func TestAddSecureNote_CreatesNoteNamedByLandingPath(t *testing.T) {
	projectRoot := t.TempDir()
	target, err := NewPSyncTarget("dec", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, projectRoot, path.Join(target.LocalRoot, "config/private.yaml"), "token: abc\n")

	client := &StubClient{}
	if err := AddSecureNote(t.Context(), client, projectRoot, target, "config/private.yaml"); err != nil {
		t.Fatalf("AddSecureNote() = %v", err)
	}
	notes := client.NotesByFolder[target.Address]
	if len(notes) != 1 {
		t.Fatalf("notes = %#v", notes)
	}
	if notes[0].RelativePath != "config/private.yaml" {
		t.Fatalf("note 名 = %q, 期望相对同步根", notes[0].RelativePath)
	}
	if notes[0].Content != "token: abc\n" {
		t.Fatalf("正文 = %q", notes[0].Content)
	}
}

func TestAddSecureNote_RejectsPathOutsideProject(t *testing.T) {
	target, err := NewPSyncTarget("tencent-cloud", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	err = AddSecureNote(t.Context(), &StubClient{}, t.TempDir(), target, "../outside.yaml")
	if err == nil {
		t.Fatal("落地路径逃逸同步根时应报错")
	}
}

func TestAddSecureNote_RejectsDecOverlap(t *testing.T) {
	projectRoot := t.TempDir()
	target, err := NewPSyncTarget("evil", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, projectRoot, ".dec/config.yaml", "x")

	err = AddSecureNote(t.Context(), &StubClient{}, projectRoot, target, "../../.dec/config.yaml")
	if err == nil {
		t.Fatal("落地路径逃逸同步根进 .dec/ 时应报错")
	}
}
