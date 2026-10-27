package assets

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/cloud/ssot"
)

// seedEditBundle 写入一个带 skill（含可执行脚本）与 rule 的 bundle，返回 revision。
func seedEditBundle(t *testing.T, w *Writer) string {
	t.Helper()
	manifest := []byte(`name: woa
items:
  - kind: skill
    path: skills/demo
    name: demo
    planes: [user, project]
  - kind: rule
    path: rules/style.mdc
    name: style
    planes: [project]
`)
	out, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files: []AssetFile{
			{Path: "skills/demo/SKILL.md", Content: []byte("# Demo\n步骤一\n步骤二\n")},
			{Path: "skills/demo/run.sh", Content: []byte("echo hi\n"), Executable: true},
			{Path: "rules/style.mdc", Content: []byte("rule body\n")},
		},
		Manifest: manifest,
		Author:   author(),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return out.Revision
}

func filesByPath(t *testing.T, w *Writer) map[string]AssetFile {
	t.Helper()
	files, err := w.ReadBundleFiles("woa")
	if err != nil {
		t.Fatalf("ReadBundleFiles: %v", err)
	}
	out := map[string]AssetFile{}
	for _, f := range files {
		out[f.Path] = f
	}
	return out
}

// 只改目标文件的目标片段，其余文件、manifest、可执行位都不能被波及。
// 这是 EditFile 存在的全部意义：调用方不传其余内容，也就不可能误删。
func TestEditFileOnlyTouchesTarget(t *testing.T) {
	w, _ := newTestWriter(t)
	base := seedEditBundle(t, w)
	before := filesByPath(t, w)

	out, err := w.EditFile(EditFileInput{
		Bundle: "woa", Path: "skills/demo/SKILL.md",
		OldString: "步骤二", NewString: "步骤二（已修正）",
		BaseRevision: base, Message: "修正步骤二", Author: author(),
	})
	if err != nil {
		t.Fatalf("EditFile: %v", err)
	}
	if out.Revision == "" || out.Revision == base {
		t.Fatalf("应产生新 revision，实际 %q", out.Revision)
	}

	after := filesByPath(t, w)
	if len(after) != len(before) {
		t.Fatalf("文件数变化：%d -> %d", len(before), len(after))
	}
	if got := string(after["skills/demo/SKILL.md"].Content); got != "# Demo\n步骤一\n步骤二（已修正）\n" {
		t.Fatalf("目标文件内容错误：%q", got)
	}
	for _, p := range []string{"skills/demo/run.sh", "rules/style.mdc", ssot.FileManifest} {
		if !bytes.Equal(before[p].Content, after[p].Content) {
			t.Fatalf("%s 不应被改动", p)
		}
	}
	if !after["skills/demo/run.sh"].Executable {
		t.Fatal("可执行位被丢失")
	}
}

// 编辑可执行脚本本身时，可执行位必须保留。
func TestEditFileKeepsExecutableBit(t *testing.T) {
	w, _ := newTestWriter(t)
	seedEditBundle(t, w)
	if _, err := w.EditFile(EditFileInput{
		Bundle: "woa", Path: "skills/demo/run.sh",
		OldString: "echo hi", NewString: "echo hello", Author: author(),
	}); err != nil {
		t.Fatalf("EditFile: %v", err)
	}
	f := filesByPath(t, w)["skills/demo/run.sh"]
	if !f.Executable || string(f.Content) != "echo hello\n" {
		t.Fatalf("脚本编辑结果错误：exec=%v content=%q", f.Executable, f.Content)
	}
}

// 匹配不唯一时必须拒绝，并且不能留下任何提交。
func TestEditFileRequiresUniqueMatch(t *testing.T) {
	w, _ := newTestWriter(t)
	base := seedEditBundle(t, w)
	cases := map[string]string{
		"零次": "不存在的片段",
		"多次": "步骤",
	}
	for name, old := range cases {
		_, err := w.EditFile(EditFileInput{
			Bundle: "woa", Path: "skills/demo/SKILL.md",
			OldString: old, NewString: "x", Author: author(),
		})
		if !errors.Is(err, ErrEditMismatch) {
			t.Fatalf("%s：期望 ErrEditMismatch，实际 %v", name, err)
		}
	}
	if head := w.store.Head(); head != base {
		t.Fatalf("失败的编辑不应产生提交：%s -> %s", base, head)
	}
}

func TestEditFileRejectsInvalidRequests(t *testing.T) {
	w, _ := newTestWriter(t)
	seedEditBundle(t, w)
	cases := []struct {
		name string
		in   EditFileInput
		want error
	}{
		{"manifest", EditFileInput{Bundle: "woa", Path: "manifest.yaml", OldString: "woa", NewString: "x"}, ErrSchema},
		{"空 old", EditFileInput{Bundle: "woa", Path: "skills/demo/SKILL.md", OldString: "", NewString: "x"}, ErrEditMismatch},
		{"无改动", EditFileInput{Bundle: "woa", Path: "skills/demo/SKILL.md", OldString: "步骤一", NewString: "步骤一"}, ErrEditMismatch},
		{"新增文件", EditFileInput{Bundle: "woa", Path: "skills/demo/new.md", OldString: "a", NewString: "b"}, ssot.ErrNotFound},
		{"不属于条目", EditFileInput{Bundle: "woa", Path: "other.md", OldString: "a", NewString: "b"}, ssot.ErrNotFound},
		{"bundle 不存在", EditFileInput{Bundle: "nope", Path: "skills/demo/SKILL.md", OldString: "a", NewString: "b"}, ssot.ErrNotFound},
		{"过期 revision", EditFileInput{Bundle: "woa", Path: "skills/demo/SKILL.md", OldString: "步骤一", NewString: "x", BaseRevision: "deadbeef"}, ssot.ErrRevisionMismatch},
	}
	for _, c := range cases {
		c.in.Author = author()
		if _, err := w.EditFile(c.in); !errors.Is(err, c.want) {
			t.Fatalf("%s：期望 %v，实际 %v", c.name, c.want, err)
		}
	}
	if _, err := w.EditFile(EditFileInput{
		Bundle: "woa", Path: "../escape", OldString: "a", NewString: "b", Author: author(),
	}); err == nil {
		t.Fatal("上跳路径应被拒绝")
	}
}

// 与整体写入相同的内容校验：编辑也不能写进明文密钥或引用不存在的密钥。
func TestEditFileRunsContentChecks(t *testing.T) {
	w, _ := newTestWriter(t)
	seedEditBundle(t, w)
	if _, err := w.EditFile(EditFileInput{
		Bundle: "woa", Path: "skills/demo/SKILL.md",
		OldString: "步骤一", NewString: "token=ghp_abcdefghijklmnopqrstuvwxyz0123456789", Author: author(),
	}); !errors.Is(err, ErrPlaintextSecret) {
		t.Fatalf("明文密钥应被拒绝，实际 %v", err)
	}
	if _, err := w.EditFile(EditFileInput{
		Bundle: "woa", Path: "skills/demo/SKILL.md",
		OldString: "步骤一", NewString: "{{secret:not-exist}}", Author: author(),
	}); !errors.Is(err, ErrMissingSecret) {
		t.Fatalf("引用不存在的密钥应被拒绝，实际 %v", err)
	}
	if got := string(filesByPath(t, w)["skills/demo/SKILL.md"].Content); !strings.Contains(got, "步骤一") {
		t.Fatalf("被拒绝的编辑不应落盘：%q", got)
	}
}
